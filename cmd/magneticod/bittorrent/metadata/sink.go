package metadata

import (
	"bytes"
	"fmt"
	"github.com/boramalper/magnetico/cmd/magneticod/dht/mainline"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/boramalper/magnetico/cmd/magneticod/dht"
	"github.com/boramalper/magnetico/pkg/persistence"
	"github.com/boramalper/magnetico/pkg/util"
)

var (
	StatsPrintClock = 10 * time.Second
)

type Metadata struct {
	InfoHash mainline.Infohash
	// Name should be thought of "Title" of the torrent. For single-file torrents, it is the name
	// of the file, and for multi-file torrents, it is the name of the root directory.
	Name         string
	TotalSize    uint64
	DiscoveredOn int64
	// Files must be populated for both single-file and multi-file torrents!
	Files []persistence.File

	Peers map[string]int64
}

type InfoHashToSink struct{
	addresses []net.TCPAddr
	originalAddresses map[string]int64 // peer ==> discoveredTimeUnixNano
}

type Sink struct {
	PeerID      []byte
	deadline    time.Duration
	maxNLeeches int
	drain       chan Metadata

	incomingInfoHashes   map[mainline.Infohash]*InfoHashToSink
	incomingInfoHashesMx sync.RWMutex

	terminated  bool
	termination chan interface{}

	deleted int

	stats sinkStats
}

func randomID() []byte {
	/* > The peer_id is exactly 20 bytes (characters) long.
	 * >
	 * > There are mainly two conventions how to encode client and client version information into the peer_id,
	 * > Azureus-style and Shadow's-style.
	 * >
	 * > Azureus-style uses the following encoding: '-', two characters for client id, four ascii digits for version
	 * > number, '-', followed by random numbers.
	 * >
	 * > For example: '-AZ2060-'...
	 *
	 * https://wiki.theory.org/index.php/BitTorrentSpecification
	 *
	 * We encode the version number as:
	 *  - First two digits for the major version number
	 *  - Last two digits for the minor version number
	 *  - Patch version number is not encoded.
	 */
	prefix := []byte("-MC0008-")

	var rando []byte
	for i := 20 - len(prefix); i >= 0; i-- {
		rando = append(rando, randomDigit())
	}

	return append(prefix, rando...)
}

func randomDigit() byte {
	var max, min int
	max, min = '9', '0'
	return byte(rand.Intn(max-min) + min)
}

func NewSink(deadline time.Duration, maxNLeeches int) *Sink {
	ms := new(Sink)

	ms.PeerID = randomID()
	ms.deadline = deadline
	ms.maxNLeeches = maxNLeeches
	ms.drain = make(chan Metadata, 10)
	ms.incomingInfoHashes = make(map[mainline.Infohash]*InfoHashToSink)
	ms.termination = make(chan interface{})
	ms.stats = sinkStats{
		requestTypes:make(map[string]int),
	}

	go ms.printStats()
	go func() {
		for range time.Tick(deadline) {
			fmt.Println("GET lock incomingInfoHashesMx for Sink status")
			ms.incomingInfoHashesMx.RLock()
			l := len(ms.incomingInfoHashes)
			ms.incomingInfoHashesMx.RUnlock()
			fmt.Println("RELEASE lock incomingInfoHashesMx for Sink status")
			zap.L().Info("Sink status",
				zap.Int("activeLeeches", l),
				zap.Int("nDeleted", ms.deleted),
				zap.Int("drainQueue", len(ms.drain)),
			)
			ms.deleted = 0
		}
	}()

	return ms
}

func (ms *Sink) Sink(res dht.Result) {
	ms.stats.AddRequest()

	if ms.terminated {
		zap.L().Panic("Trying to Sink() an already closed Sink!")
	}
	ms.incomingInfoHashesMx.Lock()
	defer ms.incomingInfoHashesMx.Unlock()

	// cap the max # of leeches
	if len(ms.incomingInfoHashes) >= ms.maxNLeeches {
		return
	}

	infoHash := res.InfoHash()
	peerAddrs := res.PeerAddrs()
	originalAddresses := make(map[string]int64, len(peerAddrs))
	for _,peer := range peerAddrs{
		//originalAddresses[peer.IP.String()+":"+strconv.Itoa(peer.Port)] = time.Now().UnixNano() //use Nano for precision
		originalAddresses[peer.String()] = time.Now().UnixNano() //use Nano for precision
	}

	if _, exists := ms.incomingInfoHashes[infoHash]; exists {
		ms.stats.AddRequestType("existingInfohash")
		return
	} else if len(peerAddrs) > 0 {
		ms.stats.AddRequestType("newInfohash")
		peer := peerAddrs[0]
		ms.incomingInfoHashes[infoHash] = &InfoHashToSink{
			addresses:peerAddrs[1:],
			originalAddresses:originalAddresses,
		}

		go NewLeech(infoHash, &peer, originalAddresses, ms.PeerID, LeechEventHandlers{
			OnSuccess: ms.flush,
			OnError:   ms.onLeechError,
		}).Do(time.Now().Add(ms.deadline))
	}

	ms.stats.AddSunk()
	zap.L().Debug("Sunk!", zap.Int("leeches", len(ms.incomingInfoHashes)), util.HexField("infoHash", infoHash[:]))
}

func (ms *Sink) Drain() <-chan Metadata {
	if ms.terminated {
		zap.L().Panic("Trying to Drain() an already closed Sink!")
	}
	return ms.drain
}

func (ms *Sink) Terminate() {
	ms.terminated = true
	close(ms.termination)
	close(ms.drain)
}

func (ms *Sink) flush(result Metadata) {
	if ms.terminated {
		return
	}

	// Delete the infoHash from ms.incomingInfoHashes ONLY AFTER once we've flushed the
	// metadata!
	ms.incomingInfoHashesMx.Lock()
	defer ms.incomingInfoHashesMx.Unlock()

	result.Peers = ms.incomingInfoHashes[result.InfoHash].originalAddresses
	delete(ms.incomingInfoHashes, result.InfoHash)

	ms.drain <- result
	ms.stats.AddDrained()
}

func (ms *Sink) onLeechError(infoHash [20]byte, err error) {
	zap.L().Debug("leech error", util.HexField("infoHash", infoHash[:]), zap.Error(err))

	ms.incomingInfoHashesMx.Lock()
	defer ms.incomingInfoHashesMx.Unlock()

	ihToSink := ms.incomingInfoHashes[infoHash]
	if len(ihToSink.addresses) > 0 {
		peer := ihToSink.addresses[0]
		ihToSink.addresses = ihToSink.addresses[1:]
		go NewLeech(infoHash, &peer, ihToSink.originalAddresses, ms.PeerID, LeechEventHandlers{
			OnSuccess: ms.flush,
			OnError:   ms.onLeechError,
		}).Do(time.Now().Add(ms.deadline))
	} else {
		ms.deleted++
		delete(ms.incomingInfoHashes, infoHash)
	}
}

func (ms *Sink) printStats(){
	for {
		time.Sleep(StatsPrintClock)
		ms.stats.Lock()
		requestsTypeString := bytes.Buffer{}
		for requestType, requestAmount := range ms.stats.requestTypes{
			requestsTypeString.WriteString(requestType+": "+strconv.Itoa(requestAmount)+",")
		}
		zap.L().Info("Sink stats (on "+StatsPrintClock.String()+"):",
			zap.String("requestTotal",strconv.Itoa(ms.stats.requestTotal)),
			zap.String("requestTypes:",requestsTypeString.String()),
			zap.String("sunkTotal",strconv.Itoa(ms.stats.sunkTotal)),
			zap.String("drained",strconv.Itoa(ms.stats.drained)+"(" +
				""+strconv.FormatFloat(float64(ms.stats.drained)/StatsPrintClock.Seconds(), 'f', -1, 64)+" drain/s )"),
		)
		ms.stats.Reset()
		ms.stats.Unlock()
	}
}

type sinkStats struct {
	sync.RWMutex
	requestTotal int
	requestTypes map[string]int
	sunkTotal    int
	drained int
}
func(ss *sinkStats) Reset(){
	//DO NOT LOCK (called in an already locked function)

	ss.requestTotal = 0
	ss.sunkTotal = 0
	ss.drained = 0
	ss.requestTypes = make(map[string]int)
}
func(ss *sinkStats) AddRequest(){
	ss.Lock()
	ss.requestTotal++
	ss.Unlock()
}
func(ss *sinkStats) AddSunk(){
	ss.Lock()
	ss.sunkTotal++
	ss.Unlock()
}
func(ss *sinkStats) AddDrained(){
	ss.Lock()
	ss.drained++
	ss.Unlock()
}
func(ss *sinkStats) AddRequestType(iType string){
	ss.Lock()
	if _,ok := ss.requestTypes[iType] ; ok{
		ss.requestTypes[iType]++
	}else{
		ss.requestTypes[iType] = 1
	}
	ss.Unlock()
}