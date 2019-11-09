package mainline

import (
	"go.uber.org/zap"
	"sync"
	"time"
)

const(
	CheckTooOldLoopTickTime = time.Hour
)

var(
	//MaxPeerLife is the max life time of a peer for a specific hash
	MaxPeerLife = 2*24*time.Hour
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
func (ipa *infohashPeersAssociation) DeleteTooOld(){
	ipa.Lock()
	defer ipa.Unlock()
	for infohash,peers := range ipa.association{
		for addr,lastSeen := range peers{
			if lastSeen < time.Now().Add(-MaxPeerLife).UnixNano(){
				delete(peers,addr)
			}
		}
		if len(peers) == 0{
			delete(ipa.association,infohash)
		}
	}
}
func (ipa *infohashPeersAssociation) StartTooOldLoop(){
	for{
		select {
		case <- time.Tick(CheckTooOldLoopTickTime):{
			zap.L().Info("Checking too old infohash-peers associations")
			ipa.DeleteTooOld()
		}
		}
	}
}


func NewInfoHashPeersAssociation() *infohashPeersAssociation{
	newIpa := &infohashPeersAssociation{
		association: make(map[Infohash]peerAddressLastSeen),
	}

	go newIpa.StartTooOldLoop()

	return newIpa
}
