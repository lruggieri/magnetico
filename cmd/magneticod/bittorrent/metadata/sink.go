package metadata

import (
	"github.com/boramalper/magnetico/cmd/magneticod/dht/mainline"
	"math/rand"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/boramalper/magnetico/cmd/magneticod/dht"
	"github.com/boramalper/magnetico/pkg/persistence"
	"github.com/boramalper/magnetico/pkg/util"
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

	CurrentTotalPeers int
}

type InfoHashToSink struct{
	addresses []net.TCPAddr
	currentTotalPeers int
}

type Sink struct {
	PeerID      []byte
	deadline    time.Duration
	maxNLeeches int
	drain       chan Metadata

	incomingInfoHashes   map[mainline.Infohash]*InfoHashToSink
	incomingInfoHashesMx sync.Mutex

	terminated  bool
	termination chan interface{}

	deleted int
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

	go func() {
		for range time.Tick(deadline) {
			ms.incomingInfoHashesMx.Lock()
			l := len(ms.incomingInfoHashes)
			ms.incomingInfoHashesMx.Unlock()
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
	currentTotalPeers := res.CurrentTotalPeers()

	if _, exists := ms.incomingInfoHashes[infoHash]; exists {
		return
	} else if len(peerAddrs) > 0 {
		peer := peerAddrs[0]
		ms.incomingInfoHashes[infoHash] = &InfoHashToSink{
			addresses:peerAddrs[1:],
			currentTotalPeers:currentTotalPeers,
		}

		go NewLeech(infoHash, &peer, currentTotalPeers, ms.PeerID, LeechEventHandlers{
			OnSuccess: ms.flush,
			OnError:   ms.onLeechError,
		}).Do(time.Now().Add(ms.deadline))
	}

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

	result.CurrentTotalPeers = ms.incomingInfoHashes[result.InfoHash].currentTotalPeers
	delete(ms.incomingInfoHashes, result.InfoHash)

	ms.drain <- result
}

func (ms *Sink) onLeechError(infoHash [20]byte, err error) {
	zap.L().Debug("leech error", util.HexField("infoHash", infoHash[:]), zap.Error(err))

	ms.incomingInfoHashesMx.Lock()
	defer ms.incomingInfoHashesMx.Unlock()

	ihToSink := ms.incomingInfoHashes[infoHash]
	if len(ihToSink.addresses) > 0 {
		peer := ihToSink.addresses[0]
		ihToSink.addresses = ihToSink.addresses[1:]
		go NewLeech(infoHash, &peer, ihToSink.currentTotalPeers, ms.PeerID, LeechEventHandlers{
			OnSuccess: ms.flush,
			OnError:   ms.onLeechError,
		}).Do(time.Now().Add(ms.deadline))
	} else {
		ms.deleted++
		delete(ms.incomingInfoHashes, infoHash)
	}
}
