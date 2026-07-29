package nfo

import "encoding/xml"

// MusicVideoNFO is Kodi's native per-video music-video NFO schema.
type MusicVideoNFO struct {
	XMLName         xml.Name `xml:"musicvideo"`
	Title           string   `xml:"title"`
	Artist          string   `xml:"artist"`
	Album           string   `xml:"album,omitempty"`
	Year            int      `xml:"year,omitempty"`
	FilenameAndPath string   `xml:"filenameandpath"`
}

// MarshalMusicVideo renders a MusicVideoNFO as indented XML with the
// standard XML declaration.
func MarshalMusicVideo(n MusicVideoNFO) ([]byte, error) {
	body, err := xml.MarshalIndent(n, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}
