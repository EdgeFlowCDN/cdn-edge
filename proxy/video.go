package proxy

import (
	"strings"
	"time"
)

const (
	// PlaylistTTL is a short TTL for live playlist files (.m3u8, .mpd)
	// which change frequently during live streaming.
	PlaylistTTL = 5 * time.Second

	// SegmentTTL is a long TTL for media segments (.ts, .m4s)
	// which are immutable once created.
	SegmentTTL = 1 * time.Hour
)

// VideoStreamType represents the type of video streaming resource.
type VideoStreamType int

const (
	VideoStreamNone     VideoStreamType = iota
	VideoStreamPlaylist                 // .m3u8, .mpd
	VideoStreamSegment                  // .ts, .m4s
)

// DetectVideoStream determines if the request path or content type corresponds
// to a video streaming resource (HLS or DASH).
func DetectVideoStream(path string, contentType string) VideoStreamType {
	lowerPath := strings.ToLower(path)

	// Check file extension
	if strings.HasSuffix(lowerPath, ".m3u8") || strings.HasSuffix(lowerPath, ".mpd") {
		return VideoStreamPlaylist
	}
	if strings.HasSuffix(lowerPath, ".ts") || strings.HasSuffix(lowerPath, ".m4s") {
		return VideoStreamSegment
	}

	// Check content type as fallback
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "application/vnd.apple.mpegurl") ||
		strings.Contains(ct, "application/x-mpegurl") ||
		strings.Contains(ct, "application/dash+xml") {
		return VideoStreamPlaylist
	}
	if strings.Contains(ct, "video/mp2t") ||
		strings.Contains(ct, "video/iso.segment") {
		return VideoStreamSegment
	}

	return VideoStreamNone
}

// VideoStreamTTL returns the appropriate TTL for a video streaming resource type.
// Returns 0 if the resource is not a video stream, meaning normal TTL logic should apply.
func VideoStreamTTL(streamType VideoStreamType) time.Duration {
	switch streamType {
	case VideoStreamPlaylist:
		return PlaylistTTL
	case VideoStreamSegment:
		return SegmentTTL
	default:
		return 0
	}
}
