package mainline

import (
	"fmt"
	"github.com/boramalper/magnetico/pkg/util"
	"net"
	"testing"
	"time"
)

func TestInfohashPeersAssociation_Add(t *testing.T) {
	ipa := NewInfoHashPeersAssociation()

	ih,_ := StringToInfohash("d73347cd7d2dfcbd23e50d585e68dcd37fe52bbe")
	newAddr := net.UDPAddr{IP: net.ParseIP("0.0.0.0"),Port:1234}
	ipa.Add(ih, newAddr.String())

	peers := ipa.GetPeers(ih)
	if len(peers) != 1{
		t.Error("expecting to find exactly 1 peer for the infohash which was just inserted")
		t.FailNow()
	}
	var lastDiscoveredOn int64
	if discoveredOn,ok := peers[newAddr.String()] ; !ok{
		t.Error("expecting to find exactly the peer that was just inserted")
		t.FailNow()
	}else{
		if discoveredOn > time.Now().Unix(){
			t.Error("just inserted peer has an wrong time")
			t.FailNow()
		}
		lastDiscoveredOn = discoveredOn
	}

	//reinserting the same peer, expecting to find a changed discoveredOn time
	ipa.Add(ih, newAddr.String())
	if len(peers) != 1{
		t.Error("reinserted the same infohash. Expecting length to have NOT changed")
		t.FailNow()
	}
	if discoveredOn,ok := peers[newAddr.String()] ; !ok{
		t.Error("expecting to re-find exactly the peer that was just inserted")
		t.FailNow()
	}else{
		if discoveredOn <= lastDiscoveredOn {
			t.Error("just re-inserted peer has an wrong time")
			t.FailNow()
		}
	}

	//reset map
	ipa.association = make(map[Infohash]peerAddressLastSeen)

	itemsToInsert := 4242
	alreadyInserted := make(map[string]bool,itemsToInsert) //just to be sure we are not re-inserting the same hash...
	for i := 0 ; i < itemsToInsert ; i++{
		newInfoHash := util.NewRandomInfoHash()
		if _,ok := alreadyInserted[newInfoHash] ; ok{
			//this is absurdly unlucky, but hey...
			fmt.Println("your are more likely to be struck by 10 lightnings at the same time than seeing this message printed. " +
				"You could have won the lottery and instead...congrats.")
			i--
			continue
		}else{
			alreadyInserted[newInfoHash] = true
		}
		ih,_ = StringToInfohash(newInfoHash)
		ipa.Add(ih, newAddr.String())
	}
	if len(ipa.association) != itemsToInsert{
		t.Error("expecting to see exactly",itemsToInsert,"infohash inserted. Found",len(ipa.association))
		t.FailNow()
	}

}
