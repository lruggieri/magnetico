package mainline

import (
	"sync"
	"time"
)

type peerAddressLastSeen map[string]int64 //peerAddress => last seenTime

//keeps track of every peer that should have info about a specific infohash
type infohashPeersAssociation struct{
	sync.RWMutex
	association map[Infohash]peerAddressLastSeen //infohash => peerAddressLastSeen
}
func (ipa *infohashPeersAssociation) Add(iInfohash Infohash, iPeer string){
	ipa.Lock()
	defer ipa.Unlock()

	if peers,ok := ipa.association[iInfohash]; ok{
		peers[iPeer] = time.Now().UnixNano()
	}else{
		ipa.association[iInfohash] = peerAddressLastSeen{iPeer:time.Now().Unix()}
	}
}
func (ipa *infohashPeersAssociation) GetPeers(iInfohash Infohash) peerAddressLastSeen{
	ipa.RLock()
	defer ipa.RUnlock()

	if peers,ok := ipa.association[iInfohash]; ok{
		return peers
	}
	return nil
}



func NewInfoHashPeersAssociation() *infohashPeersAssociation{
	return &infohashPeersAssociation{
		association: make(map[Infohash]peerAddressLastSeen),
	}
}
