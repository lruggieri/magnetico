package mainline

import "encoding/hex"

type Infohash [20]byte
func (ih *Infohash) String() string{
	return hex.EncodeToString(ih[:])
}
