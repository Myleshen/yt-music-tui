package player

import (
	"math/rand"

	"github.com/Myleshen/yt-music-tui/internal/ytmusic"
)

// Queue manages the playback track list.
//
// Playback order is decoupled from the underlying track slice: `order` is a
// permutation of track indices that defines the sequence Next/Prev walk. With
// shuffle off, order is the identity [0,1,...,n-1]; with shuffle on, the tracks
// after the current one are shuffled while the already-played prefix and the
// current track keep their places. This means toggling shuffle never loses the
// full list and never interrupts the current track, and toggling it off
// restores the original order exactly.
type Queue struct {
	tracks  []ytmusic.Track
	current int
	shuffle bool
	order   []int
}

// NewQueue creates a queue from a track list, starting at index.
func NewQueue(tracks []ytmusic.Track, startIndex int) *Queue {
	q := &Queue{tracks: tracks, current: startIndex}
	if q.current < 0 {
		q.current = 0
	}
	if q.current >= len(q.tracks) {
		q.current = len(q.tracks) - 1
	}
	q.rebuildOrder()
	return q
}

// rebuildOrder regenerates the playback order: identity when shuffle is off,
// or identity up to and including the current track with a shuffled tail when
// shuffle is on.
func (q *Queue) rebuildOrder() {
	n := len(q.tracks)
	q.order = make([]int, n)
	for i := range q.order {
		q.order[i] = i
	}
	if q.shuffle && q.current+1 < n {
		tail := q.order[q.current+1:]
		rand.Shuffle(len(tail), func(i, j int) { tail[i], tail[j] = tail[j], tail[i] })
	}
}

// orderPos returns the current track's position within the playback order.
func (q *Queue) orderPos() int {
	for i, idx := range q.order {
		if idx == q.current {
			return i
		}
	}
	return 0
}

// Shuffled reports whether shuffle mode is on.
func (q *Queue) Shuffled() bool { return q.shuffle }

// SetShuffle turns shuffle on or off. The current track is preserved; turning
// it off restores the original (sorted) order.
func (q *Queue) SetShuffle(on bool) {
	if q.shuffle == on {
		return
	}
	q.shuffle = on
	q.rebuildOrder()
}

// Order returns a copy of the current playback order.
func (q *Queue) Order() []int {
	out := make([]int, len(q.order))
	copy(out, q.order)
	return out
}

// RestoreOrder installs a previously persisted playback order and shuffle flag.
// An order that is not a valid permutation of the track indices is ignored in
// favour of a freshly derived one, so corrupt persisted state cannot break the
// queue.
func (q *Queue) RestoreOrder(order []int, shuffle bool) {
	if validPermutation(order, len(q.tracks)) {
		q.order = append([]int(nil), order...)
		q.shuffle = shuffle
		return
	}
	q.shuffle = shuffle
	q.rebuildOrder()
}

// validPermutation reports whether order is a permutation of 0..n-1.
func validPermutation(order []int, n int) bool {
	if len(order) != n {
		return false
	}
	seen := make([]bool, n)
	for _, idx := range order {
		if idx < 0 || idx >= n || seen[idx] {
			return false
		}
		seen[idx] = true
	}
	return true
}

// Current returns the currently active track.
func (q *Queue) Current() (ytmusic.Track, bool) {
	if len(q.tracks) == 0 || q.current < 0 || q.current >= len(q.tracks) {
		return ytmusic.Track{}, false
	}
	return q.tracks[q.current], true
}

// Next advances to the next track in playback order and returns it.
func (q *Queue) Next() (ytmusic.Track, bool) {
	pos := q.orderPos()
	if pos+1 >= len(q.order) {
		return ytmusic.Track{}, false
	}
	q.current = q.order[pos+1]
	return q.Current()
}

// Prev moves to the previous track in playback order and returns it.
func (q *Queue) Prev() (ytmusic.Track, bool) {
	pos := q.orderPos()
	if pos-1 < 0 {
		return ytmusic.Track{}, false
	}
	q.current = q.order[pos-1]
	return q.Current()
}

// HasNext returns true if there is a next track.
func (q *Queue) HasNext() bool {
	return q.orderPos()+1 < len(q.order)
}

// HasPrev returns true if there is a previous track.
func (q *Queue) HasPrev() bool {
	return q.orderPos() > 0
}

// CurrentIndex returns the 0-based index of the current track (into the
// underlying track list, not the playback order).
func (q *Queue) CurrentIndex() int {
	return q.current
}

// PeekNext returns the next track in playback order without advancing.
func (q *Queue) PeekNext() (ytmusic.Track, bool) {
	pos := q.orderPos()
	if pos+1 >= len(q.order) {
		return ytmusic.Track{}, false
	}
	return q.tracks[q.order[pos+1]], true
}

// UpcomingCount returns how many tracks remain after the current one in
// playback order.
func (q *Queue) UpcomingCount() int {
	n := len(q.order) - q.orderPos() - 1
	if n < 0 {
		return 0
	}
	return n
}

// Upcoming returns up to n tracks strictly after the current position in
// playback order, without changing the index. It backs the Now Playing
// "Up Next" panel; order is preserved and a short or exhausted queue yields
// fewer (or zero) tracks.
func (q *Queue) Upcoming(n int) []ytmusic.Track {
	if n <= 0 || len(q.order) == 0 {
		return nil
	}
	pos := q.orderPos()
	start := pos + 1
	if start >= len(q.order) {
		return nil
	}
	end := start + n
	if end > len(q.order) {
		end = len(q.order)
	}
	out := make([]ytmusic.Track, end-start)
	for i, idx := range q.order[start:end] {
		out[i] = q.tracks[idx]
	}
	return out
}

// UpcomingIndexes returns the absolute track indices of up to n tracks strictly
// after the current position in playback order, without changing the index.
func (q *Queue) UpcomingIndexes(n int) []int {
	if n <= 0 || len(q.order) == 0 {
		return nil
	}
	pos := q.orderPos()
	start := pos + 1
	if start >= len(q.order) {
		return nil
	}
	end := start + n
	if end > len(q.order) {
		end = len(q.order)
	}
	out := make([]int, end-start)
	copy(out, q.order[start:end])
	return out
}

// UpcomingIndex returns the absolute track index of the track `offset` places
// after the current one in playback order (offset 0 = the first upcoming track).
func (q *Queue) UpcomingIndex(offset int) (int, bool) {
	if offset < 0 {
		return 0, false
	}
	j := q.orderPos() + 1 + offset
	if j < 0 || j >= len(q.order) {
		return 0, false
	}
	return q.order[j], true
}

// TrackAt returns the track at absolute index i.
func (q *Queue) TrackAt(i int) (ytmusic.Track, bool) {
	if i < 0 || i >= len(q.tracks) {
		return ytmusic.Track{}, false
	}
	return q.tracks[i], true
}

// SetCurrent moves the current index to i (clamped to the valid range) and
// returns the resulting track. It is used by the Up Next panel to jump to a
// specific upcoming track.
func (q *Queue) SetCurrent(i int) (ytmusic.Track, bool) {
	if len(q.tracks) == 0 {
		return ytmusic.Track{}, false
	}
	if i < 0 {
		i = 0
	}
	if i >= len(q.tracks) {
		i = len(q.tracks) - 1
	}
	q.current = i
	return q.Current()
}

// --- queue editing -------------------------------------------------------
//
// These mutations are shuffle-aware: they reason about playback order, not the
// raw track slice. Up Next only ever exposes tracks strictly after the current
// one, so the current track is never moved or removed. When shuffle is off the
// playback order is kept as the identity by flattening an edited order back
// into the track slice, so the reordered list is what persistence stores.

// orderIndexOf returns the position of absolute track index i within the
// playback order, or -1 when i is not present.
func (q *Queue) orderIndexOf(i int) int {
	for pos, idx := range q.order {
		if idx == i {
			return pos
		}
	}
	return -1
}

// swapOrder exchanges two positions in the playback order and, when shuffle is
// off, flattens the result so the identity-order invariant is preserved.
func (q *Queue) swapOrder(a, b int) {
	q.order[a], q.order[b] = q.order[b], q.order[a]
	if !q.shuffle {
		q.flattenOrder()
	}
}

// flattenOrder rewrites the track slice to match the current playback order and
// resets the order to the identity, keeping the current track in place. It is
// only used with shuffle off, where order is conceptually the sorted list.
func (q *Queue) flattenOrder() {
	pos := q.orderPos()
	reordered := make([]ytmusic.Track, len(q.order))
	for i, idx := range q.order {
		reordered[i] = q.tracks[idx]
	}
	q.tracks = reordered
	q.current = pos
	q.order = make([]int, len(reordered))
	for i := range q.order {
		q.order[i] = i
	}
}

// MoveUp moves the track at absolute index i one position earlier in playback
// order. It reports false when i is the current track, an already-played track,
// or already the first upcoming track (so it can never cross the current one).
func (q *Queue) MoveUp(i int) bool {
	pos := q.orderIndexOf(i)
	if pos < 0 {
		return false
	}
	if pos <= q.orderPos()+1 {
		return false
	}
	q.swapOrder(pos, pos-1)
	return true
}

// MoveDown moves the track at absolute index i one position later in playback
// order. It reports false when i is the current/played track or already last.
func (q *Queue) MoveDown(i int) bool {
	pos := q.orderIndexOf(i)
	if pos < 0 {
		return false
	}
	if pos <= q.orderPos() || pos+1 >= len(q.order) {
		return false
	}
	q.swapOrder(pos, pos+1)
	return true
}

// Remove deletes the track at absolute index i from the queue. The current
// (playing) track cannot be removed; Remove reports false for it. Removing a
// track before the current one shifts the current index back so the same track
// keeps playing. The playback order is repaired accordingly.
func (q *Queue) Remove(i int) bool {
	if i < 0 || i >= len(q.tracks) || i == q.current {
		return false
	}
	q.tracks = append(q.tracks[:i], q.tracks[i+1:]...)
	order := make([]int, 0, len(q.order)-1)
	for _, idx := range q.order {
		if idx == i {
			continue
		}
		if idx > i {
			idx--
		}
		order = append(order, idx)
	}
	q.order = order
	if i < q.current {
		q.current--
	}
	return true
}

// ClearUpcoming removes every track after the current one in playback order,
// keeping the already-played prefix and the current track, and returns how many
// tracks were removed. Playback is unaffected.
func (q *Queue) ClearUpcoming() int {
	cur := q.orderPos()
	if cur+1 >= len(q.order) {
		return 0
	}
	removed := len(q.order) - (cur + 1)
	kept := make([]ytmusic.Track, cur+1)
	for k := 0; k <= cur; k++ {
		kept[k] = q.tracks[q.order[k]]
	}
	q.tracks = kept
	q.order = make([]int, cur+1)
	for k := range q.order {
		q.order[k] = k
	}
	q.current = cur
	return removed
}

// Append adds tracks to the end of the queue (keeping the current track and
// position) and returns how many were added. When shuffle is on the appended
// tracks are shuffled among themselves. It backs radio/autoplay.
func (q *Queue) Append(tracks []ytmusic.Track) int {
	if len(tracks) == 0 {
		return 0
	}
	start := len(q.tracks)
	q.tracks = append(q.tracks, tracks...)
	for i := start; i < len(q.tracks); i++ {
		q.order = append(q.order, i)
	}
	if q.shuffle {
		tail := q.order[start:]
		rand.Shuffle(len(tail), func(i, j int) { tail[i], tail[j] = tail[j], tail[i] })
	}
	return len(tracks)
}

// Neighbors returns up to `ahead` tracks following the current position and up
// to `behind` tracks preceding it (in playback order), for background
// prefetching. The current track is excluded and order is preserved (tracks
// before the current one first).
func (q *Queue) Neighbors(ahead, behind int) []ytmusic.Track {
	var out []ytmusic.Track
	if len(q.order) == 0 {
		return out
	}
	pos := q.orderPos()
	start := pos - behind
	if start < 0 {
		start = 0
	}
	end := pos + ahead
	if end >= len(q.order) {
		end = len(q.order) - 1
	}
	for i := start; i <= end; i++ {
		if i == pos || i < 0 || i >= len(q.order) {
			continue
		}
		out = append(out, q.tracks[q.order[i]])
	}
	return out
}

// Tracks returns a copy of the full track list (for state persistence).
func (q *Queue) Tracks() []ytmusic.Track {
	out := make([]ytmusic.Track, len(q.tracks))
	copy(out, q.tracks)
	return out
}

// Len returns the total number of tracks.
func (q *Queue) Len() int {
	return len(q.tracks)
}
