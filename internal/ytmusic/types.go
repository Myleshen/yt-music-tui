package ytmusic

// Track represents a YouTube Music song.
type Track struct {
	VideoID   string `json:"videoId"`
	Title     string `json:"title"`
	Artist    string `json:"artist"`
	ArtistID  string `json:"artistId,omitempty"` // browse ID of the primary artist
	Album     string `json:"album"`
	AlbumID   string `json:"albumId,omitempty"` // browse ID of the album
	Duration  string `json:"duration"`
	URL       string `json:"url"`
	Thumbnail string `json:"thumbnail"`
	// SetVideoID identifies a track's membership in a playlist; required to
	// remove it (it is only present on playlist items, not search/library rows).
	SetVideoID string `json:"setVideoId,omitempty"`
}

// Playlist represents a user playlist.
type Playlist struct {
	PlaylistID string `json:"playlistId"`
	Title      string `json:"title"`
	Count      int    `json:"count"`
}

// Collection is a playable tile from the Discover feed: a playlist, album,
// mix, or podcast. BrowseID is the raw InnerTube browse id used to open it;
// Params carries any extra browse parameters (e.g. a moods/genres category).
// PlaylistID is set when the tile resolves to a playlist or mix.
type Collection struct {
	BrowseID   string `json:"browseId,omitempty"`
	Params     string `json:"params,omitempty"`
	PlaylistID string `json:"playlistId,omitempty"`
	Title      string `json:"title"`
	Subtitle   string `json:"subtitle,omitempty"`
	Thumbnail  string `json:"thumbnail,omitempty"`
}

// Section is a titled group of the Discover feed: a carousel, shelf, or grid.
// A section holds either playable tracks or playable collections (or, for a
// description shelf, just text).
type Section struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Tracks      []Track      `json:"tracks,omitempty"`
	Collections []Collection `json:"collections,omitempty"`
}

// Rating is a YouTube Music thumbs rating for a track.
type Rating string

const (
	// RatingLike is the "thumbs up" state.
	RatingLike Rating = "LIKE"
	// RatingDislike is the "thumbs down" state.
	RatingDislike Rating = "DISLIKE"
	// RatingIndifferent clears any rating (neither liked nor disliked).
	RatingIndifferent Rating = "INDIFFERENT"
)

// Browse item kinds.
const (
	KindAlbum    = "album"
	KindArtist   = "artist"
	KindPlaylist = "playlist"
	KindVideo    = "video"
)

// BrowseItem is a navigable, non-track entry: an album, artist, playlist or
// video tile returned by a filtered search (or nested in an artist page).
type BrowseItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle,omitempty"`
	Kind      string `json:"kind"`
	Thumbnail string `json:"thumbnail,omitempty"`
}

// Artist is a short artist summary.
type Artist struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Subscribers string `json:"subscribers,omitempty"`
}

// Album is a short album summary.
type Album struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist,omitempty"`
	Year   string `json:"year,omitempty"`
}

// ArtistPage is the readable view of an artist: a header plus top songs and a
// list of albums to open.
type ArtistPage struct {
	Artist Artist       `json:"artist"`
	Tracks []Track      `json:"tracks"`
	Albums []BrowseItem `json:"albums"`
}

// AlbumPage is the readable view of an album: a header plus its tracks.
type AlbumPage struct {
	Album  Album   `json:"album"`
	Tracks []Track `json:"tracks"`
}
