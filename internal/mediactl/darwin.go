//go:build darwin

// macOS backend: MPNowPlayingInfoCenter (the Control Center / Now Playing
// widget) and MPRemoteCommandCenter (media keys, AirPods and Bluetooth
// controls), driven through the Objective-C runtime.
//
// It deliberately avoids cgo. purego calls objc_msgSend directly, which keeps
// CGO_ENABLED=0 cross-compiles working and means a plain `go build` on macOS
// needs nothing but the command line tools.
//
// AppKit only delivers remote command events on the main run loop, so the
// service implements Runner: the caller hands it the main goroutine (via
// RunLoop) and runs the TUI elsewhere. Every failure is non-fatal — a missing
// framework or a refused activation just leaves ytm without media keys.

package mediactl

import (
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const (
	// NSUTF8StringEncoding, from NSString.h.
	nsUTF8StringEncoding = 4
	// NSApplicationActivationPolicyAccessory keeps ytm out of the Dock. It is
	// also the policy mpv uses for its own AppKit integration.
	activationPolicyAccessory = 1
	// MPNowPlayingPlaybackState values.
	playbackStatePlaying = 1
	playbackStatePaused  = 2
	playbackStateStopped = 3
	// remoteCommandSuccess is MPRemoteCommandHandlerStatusCommandSucceeded.
	remoteCommandSuccess = 0
)

// Framework paths. AppKit is only needed for the NSApplication run loop that
// delivers media keys; MediaPlayer carries both API classes.
const (
	appKitPath       = "/System/Library/Frameworks/AppKit.framework/AppKit"
	mediaPlayerPath  = "/System/Library/Frameworks/MediaPlayer.framework/MediaPlayer"
	coreFoundPath    = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"
	systemLibPath    = "/usr/lib/libSystem.B.dylib"
	foundationDylib  = "/System/Library/Frameworks/Foundation.framework/Foundation"
	rtldNowAndGlobal = purego.RTLD_NOW | purego.RTLD_GLOBAL
)

// Cached selectors. RegisterName takes the Objective-C global lock, so it is
// done once at start-up rather than per message send.
var (
	selAlloc                  = objc.RegisterName("alloc")
	selInit                   = objc.RegisterName("init")
	selRelease                = objc.RegisterName("release")
	selRetain                 = objc.RegisterName("retain")
	selRespondsToSelector     = objc.RegisterName("respondsToSelector:")
	selInitWithBytesLength    = objc.RegisterName("initWithBytes:length:encoding:")
	selInitWithLongLong       = objc.RegisterName("initWithLongLong:")
	selSetObjectForKey        = objc.RegisterName("setObject:forKey:")
	selInitWithContentsOfFile = objc.RegisterName("initWithContentsOfFile:")
	selInitWithImage          = objc.RegisterName("initWithImage:")
	selSharedApplication      = objc.RegisterName("sharedApplication")
	selSetActivationPolicy    = objc.RegisterName("setActivationPolicy:")
	selFinishLaunching        = objc.RegisterName("finishLaunching")
	selRun                    = objc.RegisterName("run")
	selStop                   = objc.RegisterName("stop:")
	selSetNowPlayingInfo      = objc.RegisterName("setNowPlayingInfo:")
	selSetPlaybackState       = objc.RegisterName("setPlaybackState:")
	selSharedCommandCenter    = objc.RegisterName("sharedCommandCenter")
	selDefaultCenter          = objc.RegisterName("defaultCenter")
	selDefault                = objc.RegisterName("default")
	selPlayCommand            = objc.RegisterName("playCommand")
	selPauseCommand           = objc.RegisterName("pauseCommand")
	selTogglePlayPauseCommand = objc.RegisterName("togglePlayPauseCommand")
	selStopCommand            = objc.RegisterName("stopCommand")
	selNextTrackCommand       = objc.RegisterName("nextTrackCommand")
	selPreviousTrackCommand   = objc.RegisterName("previousTrackCommand")
	selChangePositionCommand  = objc.RegisterName("changePlaybackPositionCommand")
	selPositionTime           = objc.RegisterName("positionTime")
	selSetEnabled             = objc.RegisterName("setEnabled:")
	selAddTargetWithHandler   = objc.RegisterName("addTargetWithHandler:")
)

// nowPlayingKeys are the dictionary keys MPNowPlayingInfoCenter expects.
type nowPlayingKeys struct {
	title    objc.ID
	artist   objc.ID
	album    objc.ID
	artwork  objc.ID
	duration objc.ID
	elapsed  objc.ID
	rate     objc.ID
}

type darwinService struct {
	keys nowPlayingKeys

	// AppKit / MediaPlayer objects, created on the main thread in RunLoop.
	app           objc.ID
	commandCenter objc.ID
	infoCenter    objc.ID

	mu      sync.Mutex
	send    func(Command)
	last    *Snapshot
	pending *Snapshot
	dirty   bool

	artPath string
	art     objc.ID

	updateBlock objc.Block
	stopBlock   objc.Block
	handlers    []objc.Block

	// libSystem / CoreFoundation entry points for main-thread hops and shutdown.
	mainQueue      uintptr
	dispatchAsync  func(queue uintptr, block uintptr)
	runLoopGetMain func() uintptr
	runLoopStop    func(loop uintptr)

	stopOnce sync.Once
}

// Backend names the platform integration this build uses.
func Backend() string { return "MPNowPlayingInfoCenter (MediaPlayer)" }

// Probe loads the frameworks and resolves the MediaPlayer classes the backend
// needs, without starting AppKit or claiming the media keys. It reports whether
// media keys and Control Center integration will work on this machine.
func Probe() error {
	s := &darwinService{}
	return s.load()
}

// New loads the frameworks and prepares the command handlers. It does not touch
// AppKit yet: everything that needs the main thread happens in RunLoop.
func New() (Service, error) {
	s := &darwinService{}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *darwinService) load() error {
	mediaPlayer, err := purego.Dlopen(mediaPlayerPath, rtldNowAndGlobal)
	if err != nil {
		return fmt.Errorf("mediactl: load MediaPlayer: %w", err)
	}
	// AppKit and Foundation are optional in the sense that a missing AppKit only
	// costs media keys, but on a real Mac they are always present. Loading them
	// with RTLD_GLOBAL makes their classes visible to the Objective-C runtime.
	if _, err := purego.Dlopen(appKitPath, rtldNowAndGlobal); err != nil {
		return fmt.Errorf("mediactl: load AppKit: %w", err)
	}
	if _, err := purego.Dlopen(foundationDylib, rtldNowAndGlobal); err != nil {
		return fmt.Errorf("mediactl: load Foundation: %w", err)
	}
	libSystem, err := purego.Dlopen(systemLibPath, rtldNowAndGlobal)
	if err != nil {
		return fmt.Errorf("mediactl: load libSystem: %w", err)
	}
	coreFoundation, err := purego.Dlopen(coreFoundPath, rtldNowAndGlobal)
	if err != nil {
		return fmt.Errorf("mediactl: load CoreFoundation: %w", err)
	}

	if objc.GetClass("MPRemoteCommandCenter") == 0 {
		return fmt.Errorf("mediactl: MPRemoteCommandCenter is unavailable (macOS 10.12.2+ required)")
	}
	if objc.GetClass("MPNowPlayingInfoCenter") == 0 {
		return fmt.Errorf("mediactl: MPNowPlayingInfoCenter is unavailable")
	}

	purego.RegisterLibFunc(&s.dispatchAsync, libSystem, "dispatch_async")
	var mainQueue func() uintptr
	purego.RegisterLibFunc(&mainQueue, libSystem, "dispatch_get_main_queue")
	s.mainQueue = mainQueue()
	purego.RegisterLibFunc(&s.runLoopGetMain, coreFoundation, "CFRunLoopGetMain")
	purego.RegisterLibFunc(&s.runLoopStop, coreFoundation, "CFRunLoopStop")

	s.keys = resolveNowPlayingKeys(mediaPlayer)
	// One block per purpose, kept alive for the life of the process: creating a
	// block per tick would exhaust purego's callback budget.
	s.updateBlock = objc.NewBlock(func(objc.Block) { s.apply() })
	s.stopBlock = objc.NewBlock(func(objc.Block) { s.shutdown() })
	return nil
}

// resolveNowPlayingKeys reads the framework's string constants with dlsym and
// falls back to the documented literal values if a symbol is missing.
func resolveNowPlayingKeys(lib uintptr) nowPlayingKeys {
	lookup := func(symbol, literal string) objc.ID {
		if addr, err := purego.Dlsym(lib, symbol); err == nil && addr != 0 {
			// The symbols are exported `NSString *const` variables, so the
			// symbol address points at a pointer-sized slot holding the object.
			// The round trip through a uintptr slot avoids the uintptr →
			// unsafe.Pointer conversion `go vet` rejects.
			var slot unsafe.Pointer
			*(*uintptr)(unsafe.Pointer(&slot)) = addr
			if id := *(*objc.ID)(slot); id != 0 {
				return id
			}
		}
		return newNSString(literal)
	}
	return nowPlayingKeys{
		title:    lookup("MPMediaItemPropertyTitle", "title"),
		artist:   lookup("MPMediaItemPropertyArtist", "artist"),
		album:    lookup("MPMediaItemPropertyAlbumTitle", "albumTitle"),
		artwork:  lookup("MPMediaItemPropertyArtwork", "artwork"),
		duration: lookup("MPMediaItemPropertyPlaybackDuration", "playbackDuration"),
		elapsed:  lookup("MPNowPlayingInfoPropertyElapsedPlaybackTime", "elapsedPlaybackTime"),
		rate:     lookup("MPNowPlayingInfoPropertyPlaybackRate", "playbackRate"),
	}
}

// --- Service --------------------------------------------------------------

// SetSend installs the callback used to relay media-key presses to the TUI.
func (s *darwinService) SetSend(send func(Command)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.send = send
	s.mu.Unlock()
}

func (s *darwinService) dispatch(cmd Command) {
	s.mu.Lock()
	send := s.send
	s.mu.Unlock()
	if send != nil {
		send(cmd)
	}
}

// Update publishes a snapshot. Cheap snapshots (idle ticks with an unchanged
// position) never leave this goroutine; anything else hops to the main queue
// with a block that survives the hop.
func (s *darwinService) Update(snap Snapshot) {
	if s == nil {
		return
	}
	s.mu.Lock()
	prev := s.last
	cur := snap
	s.last = &cur
	changed := needsInfoRefresh(prev, &cur)
	if changed {
		pending := cur
		s.pending = &pending
		s.dirty = true
	}
	ready := s.infoCenter != 0
	s.mu.Unlock()

	if !changed || !ready || s.dispatchAsync == nil {
		return
	}
	s.dispatchAsync(s.mainQueue, uintptr(s.updateBlock))
}

// Close stops the run loop (when one is running) and drops the published
// metadata so the system stops advertising ytm.
func (s *darwinService) Close() {
	if s == nil {
		return
	}
	s.StopLoop()
}

// --- Runner ---------------------------------------------------------------

// RunLoop installs the AppKit integration and blocks on the main run loop until
// StopLoop is called. It must run on the process main thread.
func (s *darwinService) RunLoop() {
	if s == nil {
		return
	}
	runtime.LockOSThread()

	app := msg(objc.ID(objc.GetClass("NSApplication")), selSharedApplication)
	if app == 0 {
		slog.Error("mediactl: NSApplication unavailable; media keys disabled")
		return
	}
	s.mu.Lock()
	s.app = app
	s.mu.Unlock()

	// Accessory: no Dock icon, but AppKit still routes remote commands here.
	msg(app, selSetActivationPolicy, int64(activationPolicyAccessory))
	msg(app, selFinishLaunching)

	if err := s.setup(); err != nil {
		slog.Error("mediactl: macOS now-playing unavailable", "err", err)
		return
	}
	s.claimNowPlaying()
	s.flush()
	msg(app, selRun)
}

// StopLoop unblocks RunLoop. The block runs on the main queue so NSApplication
// is stopped from its own thread; the CoreFoundation call is a belt-and-braces
// wake-up for the case where the block is never serviced.
func (s *darwinService) StopLoop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.dispatchAsync != nil && s.stopBlock != 0 {
			s.dispatchAsync(s.mainQueue, uintptr(s.stopBlock))
		}
		if s.runLoopStop != nil && s.runLoopGetMain != nil {
			s.runLoopStop(s.runLoopGetMain())
		}
	})
}

// setup creates the media player singletons and wires the remote commands.
func (s *darwinService) setup() error {
	center := msg(objc.ID(objc.GetClass("MPRemoteCommandCenter")), selSharedCommandCenter)
	if center == 0 {
		return fmt.Errorf("mediactl: MPRemoteCommandCenter unavailable")
	}
	info := s.firstClassMethod(objc.GetClass("MPNowPlayingInfoCenter"), selDefaultCenter, selDefault)
	if info == 0 {
		return fmt.Errorf("mediactl: MPNowPlayingInfoCenter unavailable")
	}

	s.mu.Lock()
	s.commandCenter = center
	s.infoCenter = info
	s.mu.Unlock()

	s.installHandlers(center)
	slog.Info("macOS now-playing backend active")
	return nil
}

// firstClassMethod sends the first selector the class responds to, which keeps
// the backend working across the defaultCenter/default rename.
func (s *darwinService) firstClassMethod(class objc.Class, selectors ...objc.SEL) objc.ID {
	cls := objc.ID(class)
	for _, sel := range selectors {
		if respondsTo(cls, sel) {
			if obj := msg(cls, sel); obj != 0 {
				return obj
			}
		}
	}
	return 0
}

// installHandlers maps MPRemoteCommandCenter commands onto mediactl actions.
func (s *darwinService) installHandlers(center objc.ID) {
	bindings := []struct {
		selector objc.SEL
		action   Action
	}{
		{selPlayCommand, ActionPlay},
		{selPauseCommand, ActionPause},
		{selTogglePlayPauseCommand, ActionPlayPause},
		{selStopCommand, ActionStop},
		{selNextTrackCommand, ActionNext},
		{selPreviousTrackCommand, ActionPrevious},
	}
	for _, b := range bindings {
		cmd := msg(center, b.selector)
		if cmd == 0 {
			continue
		}
		action := b.action
		block := objc.NewBlock(func(objc.Block, objc.ID) int64 {
			s.dispatch(Command{Action: action})
			return remoteCommandSuccess
		})
		msg(cmd, selSetEnabled, uintptr(1))
		msg(cmd, selAddTargetWithHandler, block)
		s.handlers = append(s.handlers, block)
	}

	// Dragging the progress slider in Control Center.
	if cmd := msg(center, selChangePositionCommand); cmd != 0 {
		block := objc.NewBlock(func(_ objc.Block, event objc.ID) int64 {
			seconds := objc.Send[float64](event, selPositionTime)
			if seconds < 0 {
				seconds = 0
			}
			s.dispatch(Command{
				Action:   ActionSetPosition,
				Position: time.Duration(seconds * float64(time.Second)),
			})
			return remoteCommandSuccess
		})
		msg(cmd, selSetEnabled, uintptr(1))
		msg(cmd, selAddTargetWithHandler, block)
		s.handlers = append(s.handlers, block)
	}
}

// claimNowPlaying nudges the system into treating ytm as the now-playing app by
// setting a playback state twice, which is the same trick mpv and the
// mediaremote tooling use.
func (s *darwinService) claimNowPlaying() {
	s.mu.Lock()
	info := s.infoCenter
	s.mu.Unlock()
	if info == 0 || !respondsTo(info, selSetPlaybackState) {
		return
	}
	msg(info, selSetPlaybackState, int64(playbackStatePaused))
	msg(info, selSetPlaybackState, int64(playbackStatePlaying))
}

// flush republishes the last snapshot once the info centre exists.
func (s *darwinService) flush() {
	s.mu.Lock()
	if s.last != nil {
		pending := *s.last
		s.pending = &pending
		s.dirty = true
	}
	s.mu.Unlock()
	s.apply()
}

// --- publishing state -----------------------------------------------------

// apply runs on the main queue and pushes the pending snapshot to the system.
func (s *darwinService) apply() {
	s.mu.Lock()
	snap, dirty := s.pending, s.dirty
	s.pending, s.dirty = nil, false
	info := s.infoCenter
	s.mu.Unlock()
	if !dirty || info == 0 {
		return
	}

	if snap == nil || !snap.HasTrack {
		msg(info, selSetNowPlayingInfo, objc.ID(0))
		s.setPlaybackState(playbackStateStopped)
		return
	}

	fields, ok := nowPlayingFor(*snap)
	if !ok {
		return
	}
	dict := s.infoDictionary(fields)
	if dict == 0 {
		return
	}
	msg(info, selSetNowPlayingInfo, dict)
	msg(dict, selRelease)
	s.setPlaybackState(pickState(fields.Rate == 1, playbackStatePlaying, playbackStatePaused))
}

func (s *darwinService) setPlaybackState(state int) {
	s.mu.Lock()
	info := s.infoCenter
	s.mu.Unlock()
	if info == 0 || !respondsTo(info, selSetPlaybackState) {
		return
	}
	msg(info, selSetPlaybackState, int64(state))
}

// infoDictionary builds the NSDictionary MPNowPlayingInfoCenter expects. Every
// object created here is released once the centre has taken its copy.
func (s *darwinService) infoDictionary(f nowPlayingInfo) objc.ID {
	dict := msg(msg(objc.ID(objc.GetClass("NSMutableDictionary")), selAlloc), selInit)
	if dict == 0 {
		return 0
	}

	var owned []objc.ID
	set := func(key, value objc.ID) {
		if key == 0 || value == 0 {
			return
		}
		msg(dict, selSetObjectForKey, value, key)
		owned = append(owned, value)
	}

	set(s.keys.title, newNSString(f.Title))
	set(s.keys.artist, newNSString(f.Artist))
	set(s.keys.album, newNSString(f.Album))
	set(s.keys.duration, newNumber(int64(f.Duration/time.Second)))
	set(s.keys.elapsed, newNumber(int64(f.Elapsed/time.Second)))
	set(s.keys.rate, newNumber(int64(f.Rate)))
	if art := s.artwork(f.ArtPath); art != 0 {
		// The cache owns the artwork; the dictionary takes its own reference.
		msg(art, selRetain)
		set(s.keys.artwork, art)
	}

	for _, obj := range owned {
		msg(obj, selRelease)
	}
	return dict
}

// artwork returns a cached MPMediaItemArtwork for the given image path, loading
// it on first use. It returns 0 when the platform has no initWithImage: or the
// image cannot be read.
func (s *darwinService) artwork(path string) objc.ID {
	s.mu.Lock()
	if s.artPath == path {
		cached := s.art
		s.mu.Unlock()
		return cached
	}
	s.mu.Unlock()

	image := s.loadImage(path)
	art := objc.ID(0)
	if image != 0 {
		instance := msg(objc.ID(objc.GetClass("MPMediaItemArtwork")), selAlloc)
		if respondsTo(instance, selInitWithImage) {
			art = msg(instance, selInitWithImage, image)
		}
		if art == 0 {
			msg(instance, selRelease)
		}
		msg(image, selRelease)
	}

	s.mu.Lock()
	old := s.art
	s.artPath, s.art = path, art
	s.mu.Unlock()
	if old != 0 {
		msg(old, selRelease)
	}
	return art
}

// loadImage reads an image file into an NSImage, or returns 0.
func (s *darwinService) loadImage(path string) objc.ID {
	if path == "" {
		return 0
	}
	name := newNSString(path)
	image := msg(msg(objc.ID(objc.GetClass("NSImage")), selAlloc), selInitWithContentsOfFile, name)
	msg(name, selRelease)
	return image
}

// shutdown clears the now-playing entry and stops NSApplication. It runs on the
// main queue.
func (s *darwinService) shutdown() {
	s.mu.Lock()
	app, info := s.app, s.infoCenter
	s.mu.Unlock()
	if info != 0 {
		msg(info, selSetNowPlayingInfo, objc.ID(0))
	}
	if app != 0 {
		msg(app, selStop, objc.ID(0))
	}
}

// --- Objective-C helpers --------------------------------------------------

// msg sends a message and returns the result as an object pointer. The result
// of a void method is undefined but harmless to read.
func msg(id objc.ID, sel objc.SEL, args ...any) objc.ID {
	if id == 0 {
		return 0
	}
	return objc.Send[objc.ID](id, sel, args...)
}

// respondsTo reports whether an object (or class object) implements sel.
func respondsTo(id objc.ID, sel objc.SEL) bool {
	if id == 0 {
		return false
	}
	return objc.Send[uintptr](id, selRespondsToSelector, sel) != 0
}

// newNSString builds a UTF-8 NSString from a Go string without going through a
// NUL-terminated buffer.
func newNSString(s string) objc.ID {
	obj := msg(objc.ID(objc.GetClass("NSString")), selAlloc)
	if obj == 0 {
		return 0
	}
	var ptr uintptr
	if len(s) > 0 {
		ptr = uintptr(unsafe.Pointer(unsafe.StringData(s)))
	}
	return msg(obj, selInitWithBytesLength, ptr, uintptr(len(s)), uintptr(nsUTF8StringEncoding))
}

// newNumber builds an NSNumber from an integer. Durations and rates are pushed
// as whole seconds/units; MediaPlayer reads the same doubleValue from them.
func newNumber(v int64) objc.ID {
	obj := msg(objc.ID(objc.GetClass("NSNumber")), selAlloc)
	if obj == 0 {
		return 0
	}
	return msg(obj, selInitWithLongLong, v)
}

// pickState returns playing or paused depending on cond.
func pickState(cond bool, playing, paused int) int {
	if cond {
		return playing
	}
	return paused
}
