package dht

import (
	"net"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/boramalper/magnetico/cmd/magneticod/dht/mainline"
)

const outputChannelLimit = 20
var (
	StatsPrintClock = 10 * time.Second
)

type Service interface {
	Start()
	Terminate()
}

type Result interface {
	InfoHash() [20]byte
	PeerAddrs() []net.TCPAddr
}

type Manager struct {
	stats            managerStats
	output           chan Result
	indexingServices []Service
}

func NewManager(addrs []string, interval time.Duration, maxNeighbors uint) *Manager {
	manager := new(Manager)
	manager.output = make(chan Result, outputChannelLimit)

	for _, addr := range addrs {
		service := mainline.NewIndexingService(addr, interval, maxNeighbors, mainline.IndexingServiceEventHandlers{
			OnResult: manager.onIndexingResult,
		})
		manager.indexingServices = append(manager.indexingServices, service)
		service.Start()
	}

	go manager.PrintStats()
	return manager
}

func (m *Manager) onIndexingResult(res mainline.IndexingResult) {
	m.stats.AddOutputRequest()
	select {
	case m.output <- res:{
		m.stats.AddOutputAccepted()
	}
	default:
		m.stats.AddOutputDiscarded()
		zap.L().Debug("DHT manager output ch is full, idx result dropped!")
	}
}

func (m *Manager) Output() <-chan Result {
	return m.output
}

func (m *Manager) Terminate() {
	for _, service := range m.indexingServices {
		service.Terminate()
	}
}


func (m *Manager) PrintStats() {
	for {
		time.Sleep(StatsPrintClock)
		m.stats.Lock()
		zap.L().Info("Manager stats (on "+StatsPrintClock.String()+"):",
			zap.String("output queue limit",strconv.Itoa(outputChannelLimit)),
			zap.String("output request",strconv.Itoa(m.stats.outputRequests)+"(" +
				""+strconv.FormatFloat(float64(m.stats.outputRequests)/StatsPrintClock.Seconds(), 'f', -1, 64)+" request/s ) "),
			zap.String("output request accepted",strconv.Itoa(m.stats.outputAccepted)+"(" +
				""+strconv.FormatFloat(float64(m.stats.outputAccepted)/StatsPrintClock.Seconds(), 'f', -1, 64)+" request/s ) "),
			zap.String("output request discarded",strconv.Itoa(m.stats.outputDiscarded)+"(" +
				""+strconv.FormatFloat(float64(m.stats.outputDiscarded)/StatsPrintClock.Seconds(), 'f', -1, 64)+" request/s ) "),
		)
		m.stats.Reset()
		m.stats.Unlock()
	}
}
type managerStats struct{
	sync.RWMutex

	outputRequests int
	outputAccepted int
	outputDiscarded int
}
func (ms *managerStats) Reset(){
	//DO NOT LOCK (called in an already locked function)

	ms.outputRequests = 0
	ms.outputAccepted = 0
	ms.outputDiscarded = 0
}
func (ms *managerStats) AddOutputRequest(){
	ms.Lock()
	ms.outputRequests++
	ms.Unlock()
}
func (ms *managerStats) AddOutputAccepted(){
	ms.Lock()
	ms.outputAccepted++
	ms.Unlock()
}
func (ms *managerStats) AddOutputDiscarded(){
	ms.Lock()
	ms.outputDiscarded++
	ms.Unlock()
}