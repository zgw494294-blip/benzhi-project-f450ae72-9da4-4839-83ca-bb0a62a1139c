package journal

import (
	"caption-delivery-qc/internal/domain"
	"encoding/json"
)

type Frame struct {
	Length   int          `json:"length"`
	Checksum string       `json:"checksum"`
	Event    domain.Event `json:"event"`
}

func EncodeFrame(e domain.Event) Frame {
	b, _ := json.Marshal(e)
	return Frame{Length: len(b), Checksum: domain.Hash(e), Event: e}
}
func ValidFrame(f Frame) bool { return f.Length > 0 && f.Checksum == domain.Hash(f.Event) }
