package nfo

import "encoding/xml"

// ArtistNFO is Kodi's native per-artist NFO schema.
type ArtistNFO struct {
	XMLName xml.Name `xml:"artist"`
	Name    string   `xml:"name"`
}

// MarshalArtist renders an ArtistNFO as indented XML with the standard
// XML declaration.
func MarshalArtist(n ArtistNFO) ([]byte, error) {
	body, err := xml.MarshalIndent(n, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}
