package mainline

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"
)

type InfoHashService struct{
	// Private
	protocol      *Protocol
	started       bool
	interval      time.Duration
	eventHandlers IndexingServiceEventHandlers

	nodeID []byte

	routingTable      map[string]*net.UDPAddr
	routingTableMutex sync.RWMutex
	maxNeighbors      uint

	counter          uint16
	getPeersRequests map[[2]byte][20]byte // GetPeersQuery.`t` -> infohash

	infoHashToLookFor []byte
	PeersRetrieved    map[string]bool //peers addresses map

	ctx context.Context
	cancel context.CancelFunc

	Done chan bool
}
func (is *InfoHashService) Start() {
	if is.started {
		zap.L().Panic("Attempting to Start() a mainline/IndexingService that has been already started! (Programmer error.)")
	}
	is.started = true

	is.protocol.Start()
	go is.bootstrap()

	zap.L().Info("Indexing Service started!")
	if DefaultThrottleRate > 0 {
		zap.L().Info("Throttle set to " + strconv.Itoa(DefaultThrottleRate) + " msg/s")
	}
}
func (is *InfoHashService) bootstrap() {
	bootstrappingNodes := []string{
		"router.bittorrent.com:6881",
		"dht.transmissionbt.com:6881",
		"dht.libtorrent.org:25401",
	}
	/*
	http://tracker.trackerfix.com:80/announce
	udp://9.rarbg.me:2790/announce
	udp://9.rarbg.to:2740/announce
	*/

	zap.L().Info("Bootstrapping as routing table is empty...")
	for _, node := range bootstrappingNodes {
		target := make([]byte, 20)
		_, err := rand.Read(target)
		if err != nil {
			zap.L().Panic("Could NOT generate random bytes during bootstrapping!")
		}

		addr, err := net.ResolveUDPAddr("udp", node)
		if err != nil {
			zap.L().Error("Could NOT resolve (UDP) address of the bootstrapping node!",
				zap.String("node", node))
			continue
		}

		is.protocol.SendMessage(NewFindNodeQuery(is.nodeID, target), addr)
	}
}

func (is *InfoHashService) Terminate() {
	is.protocol.Terminate()
}

func NewInfoHashService(
	laddr string,
	interval time.Duration,
	maxNeighbors uint,
	eventHandlers IndexingServiceEventHandlers,
	hashToLookFor []byte) *InfoHashService {
	service := new(InfoHashService)
	service.interval = interval
	service.protocol = NewProtocol(
		laddr,
		ProtocolEventHandlers{
			OnFindNodeResponse:         service.onFindNodeResponse,
			OnGetPeersResponse:			service.onGetPeersResponse,
		},
	)
	service.nodeID = make([]byte, 20)
	service.routingTable = make(map[string]*net.UDPAddr)
	service.maxNeighbors = maxNeighbors
	service.eventHandlers = eventHandlers

	service.infoHashToLookFor = hashToLookFor

	service.getPeersRequests = make(map[[2]byte][20]byte)

	service.PeersRetrieved = make(map[string]bool)

	service.Done = make(chan bool)

	ctx, cancel := context.WithCancel(context.Background())
	service.ctx = ctx
	service.cancel = cancel

	return service
}
func (is *InfoHashService) onFindNodeResponse(response *Message, addr *net.UDPAddr) {
	//if we are done
	if is.ctx.Err() != nil{
		return
	}

	is.routingTableMutex.Lock()
	defer is.routingTableMutex.Unlock()

	for _, node := range response.R.Nodes {
		if uint(len(is.routingTable)) >= is.maxNeighbors {
			//fmt.Println("reached max neighbours")
			is.cancel()
			is.Done <- true
			break
		}else{
			if len(is.routingTable) % 100 == 0{
				fmt.Println(len(is.routingTable))
			}
		}
		if node.Addr.Port == 0 { // Ignore nodes who "use" port 0.
			continue
		}

		is.routingTable[string(node.ID)] = &node.Addr

		target := make([]byte, 20)
		_, err := rand.Read(target)
		if err != nil {
			zap.L().Panic("Could NOT generate random bytes!")
		}
		newAddress := node.Addr
		is.protocol.SendMessage(NewFindNodeQuery(is.nodeID, target), &newAddress)

		msg := NewGetPeersQuery(is.nodeID, is.infoHashToLookFor)
		t := uint16BE(1)
		msg.T = t[:]

		is.protocol.SendMessage(msg, &newAddress)
	}
}

func (is *InfoHashService) onGetPeersResponse(msg *Message, addr *net.UDPAddr) {
	//if we are done
	if is.ctx.Err() != nil{
		return
	}

	var t [2]byte
	copy(t[:], msg.T)

	// BEP 51 specifies that
	//     The new sample_infohashes remote procedure call requests that a remote node return a string of multiple
	//     concatenated infohashes (20 bytes each) FOR WHICH IT HOLDS GET_PEERS VALUES.
	//                                                                          ^^^^^^
	// So theoretically we should never hit the case where `values` is empty, but c'est la vie.
	// BUT! If we shot a get peers query to a host with a specific infohash just to fetch information about it...most
	// of the times it will be empty
	if len(msg.R.Values) == 0 {
		return
	}

	respType := beUint16(t)
	fmt.Println("GOT PEER RESPONSE TYPE",respType, "FROM",addr.String())
	if respType == 2{
		fmt.Println("\t\t!!!!!!!!!")
	}

	peerAddrs := make([]net.TCPAddr, 0)
	for _, peer := range msg.R.Values {
		if peer.Port == 0 {
			continue
		}

		newPeerAddr := net.TCPAddr{
			IP:   peer.IP,
			Port: peer.Port,
		}
		peerAddrs = append(peerAddrs, newPeerAddr)
		is.PeersRetrieved[newPeerAddr.String()] = true
	}

	fmt.Println("Got",len(peerAddrs),"peers. Current total peers:",len(is.PeersRetrieved))

	/*if msg.R.BFsd != nil{
		var seedsInfo []byte
		err := msg.R.BFsd.UnmarshalJSON(seedsInfo)
		if err != nil{
			fmt.Println("error unmarshalling seeds",err)
		}
		fmt.Println("\t\t\t\t\t\t"+string(seedsInfo))
	}
	if msg.R.BFpe != nil{
		var  peersInfo []byte
		err := msg.R.BFpe.UnmarshalJSON(peersInfo)
		if err != nil{
			fmt.Println("error unmarshalling peers",err)
		}
		fmt.Println("\t\t\t\t\t\t"+string(peersInfo))
	}*/

	for _,peerAddr := range peerAddrs{
		msg := NewGetPeersQuery(is.nodeID, is.infoHashToLookFor)
		t := uint16BE(2)
		msg.T = t[:]
		newUDPAddr := &net.UDPAddr{
			IP:peerAddr.IP.To4(),
			Port:peerAddr.Port,
		}
		is.protocol.SendMessage(msg, newUDPAddr)
		var infoHash [20]byte
		copy(infoHash[:], is.infoHashToLookFor)
		is.getPeersRequests[t] = infoHash
		is.counter++
	}

	/*is.eventHandlers.OnResult(IndexingResult{
		infoHash:  infoHash,
		peerAddrs: peerAddrs,
	})*/
}