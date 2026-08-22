package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
)

func CueDigest(cues map[string]Cue) string {
	ids := make([]string, 0, len(cues))
	for id := range cues {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	h := sha256.New()
	for _, id := range ids {
		c := cues[id]
		h.Write([]byte(id))
		h.Write([]byte(c.Text))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
func EventDigest(events []Event) string { return Hash(events) }
