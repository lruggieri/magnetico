package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"github.com/boramalper/magnetico/cmd/magneticod/bittorrent/metadata"
	"github.com/boramalper/magnetico/cmd/magneticod/dht/mainline"
	"github.com/boramalper/magnetico/pkg/util"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wessie/appdirs"
)

func TestAppdirs(t *testing.T) {
	var expected, returned string

	returned = appdirs.UserDataDir("magneticod", "", "", false)
	expected = appdirs.ExpandUser("~/.local/share/magneticod")
	if returned != expected {
		t.Errorf("UserDataDir returned an unexpected value!  `%s`", returned)
	}

	returned = appdirs.UserCacheDir("magneticod", "", "", true)
	expected = appdirs.ExpandUser("~/.cache/magneticod")
	if returned != expected {
		t.Errorf("UserCacheDir returned an unexpected value!  `%s`", returned)
	}
}

func TestGetSeedersLeeches(t *testing.T){

	hashToLookFor, err := hex.DecodeString("c1b27f59678bc7179012877135e0858f75ae6767")
	if err != nil{
		t.Error(err)
		t.FailNow()
	}


	service := mainline.NewInfoHashService("0.0.0.0:0", time.Second, 1000000, mainline.IndexingServiceEventHandlers{
		OnResult: func(result mainline.IndexingResult){
			fmt.Println("custom Indexing function")
			ih :=  result.InfoHash()
			fmt.Println("infoHash :",util.HexField("infoHash",ih[:]).String)

		},
	},hashToLookFor)
	service.Start()

	fmt.Println("OK")

	<- service.Done
	var infoHash [20]byte
	copy(infoHash[:], hashToLookFor)
	fmt.Println("performing leech check")
	nodeID := make([]byte, 20)
	for peer := range service.PeersRetrieved{
		split := strings.Split(peer,":")
		port,_ := strconv.Atoi(split[1])
		addr := net.TCPAddr{
			IP:net.ParseIP(split[0]),
			Port:port,
		}

		metadata.NewLeech(infoHash, &addr, nodeID, metadata.LeechEventHandlers{
			OnSuccess: func(mtd metadata.Metadata){
				fmt.Println("===================================================== WOW!")
			},
			OnError: func(i [20]byte, err error){
				fmt.Println("error:",err)
			},
		}).Do(time.Now().Add(5*time.Second))
	}
}


func TestBEP33SeedLeech(t *testing.T){

	hashToLookFor, err := hex.DecodeString("c1b27f59678bc7179012877135e0858f75ae6767")
	if err != nil{
		t.Error(err)
		t.FailNow()
	}

	type Msg struct{
		message *mainline.Message
		addr *net.UDPAddr
	}
	messageChan := make(chan Msg)

	protocol := mainline.NewProtocol(
		"0.0.0.0:0",
		mainline.ProtocolEventHandlers{
			OnFindNodeResponse: func(message *mainline.Message, addr *net.UDPAddr) {
				fmt.Println("FIND_NODE RESPONSE")
			},
			OnGetPeersResponse: func(message *mainline.Message, addr *net.UDPAddr) {
				var identifier [2]byte
				copy(identifier[:], message.T)
				identifierInt := beUint16(identifier)
				fmt.Println("GET_PEERS RESPONSE with T",identifierInt,"FROM",addr.IP)
				fmt.Println(message)

				if len(message.R.Values) == 0{
					fmt.Println("no values...")
					return
				}

				for _,peer := range message.R.Values{
					if peer.Port == 0 {
						fmt.Println("port 0")
						continue
					}


					msg := mainline.NewGetPeersQuery(make([]byte, 20), hashToLookFor)

					newIdentifier := uint16BE(identifierInt+1)
					msg.T = newIdentifier[:]

					messageChan <- Msg{
						message:msg,
						addr:&net.UDPAddr{
							IP:   peer.IP,
							Port: peer.Port,
						},
					}
				}
			},
		},)
	protocol.Start()

	go func(){
		for mes := range messageChan{
			fmt.Print("sending message to ",mes.addr.IP,"...")
			protocol.SendMessage(mes.message,mes.addr)
			fmt.Println("sent")
		}
	}()

	msg := mainline.NewGetPeersQuery(make([]byte, 20), hashToLookFor)
	identifier := uint16BE(1)
	msg.T = identifier[:]

	messageChan <- Msg{
		message:msg,
		addr:&net.UDPAddr{
			//dht.transmissionbt.com:6881 , resolved to 87.98.162.88
			IP:net.ParseIP("31.22.90.175"),
			Port:49001,
		},
	}

	select {

	}
}

func TestCheckSeeder(t *testing.T){

	//no get_peer response
	hastString := "d73347cd7d2dfcbd23e50d585e68dcd37fe52bbe"
	ipPortString := "39.97.119.32:39939"


	/*
	//no metadata
	hastString := "870492322614e438b1a64fbcaf7ccac4a99a02df"
	ipPortString := "101.141.137.231:25387"*/

	split := strings.Split(ipPortString,":")
	ipString := split[0]
	port,_ := strconv.Atoi(split[1])

	tcpAddr := &net.TCPAddr{IP:net.ParseIP(ipString),Port:port}
	udpAddr := &net.UDPAddr{IP: net.ParseIP(ipString),Port:port}

	hashToLookFor, err := hex.DecodeString(hastString)
	if err != nil{
		t.Error(err)
		t.FailNow()
	}
	var infohash [20]byte
	copy(infohash[:],hashToLookFor)

	myId := make([]byte, 20)


	msg := mainline.NewGetPeersQuery(myId, hashToLookFor)
	identifier := uint16BE(1)
	msg.T = identifier[:]
	response := make(chan bool)
	protocol := mainline.NewProtocol(
		"0.0.0.0:0",
		mainline.ProtocolEventHandlers{
			OnSampleInfohashesResponse:func(message *mainline.Message, addr *net.UDPAddr){
				fmt.Println("INFO_HASHES RESPONSE")
				response <- true
			},
			OnFindNodeResponse: func(message *mainline.Message, addr *net.UDPAddr) {
				fmt.Println("FIND_NODE RESPONSE")
				fmt.Println(message.R.BFsd)
				fmt.Println(message.R.BFpe)
				response <- true
			},
			OnGetPeersResponse: func(message *mainline.Message, addr *net.UDPAddr) {
				fmt.Println("GET_PEERS RESPONSE ")
				fmt.Println(message.R.BFsd)
				fmt.Println(message.R.BFpe)
				response <- true
			},
		},)
	protocol.Start()

	protocol.SendMessage(mainline.NewFindNodeQuery(myId,make([]byte,20)), udpAddr)
	protocol.SendMessage(msg, udpAddr)



	fmt.Println("waiting response...")
	select {
	case <-response:{fmt.Println("got response")}
	case <-time.Tick(2*time.Second):{fmt.Println("timeout")}
	}

	retry := 1
	for i := 0 ; i < retry ; i++{
		metadata.NewLeech(infohash, tcpAddr, myId, metadata.LeechEventHandlers{
			OnSuccess: func(mtd metadata.Metadata){
				fmt.Println("===================================================== Seed responded to metadata request")
			},
			OnError: func(i [20]byte, err error){
				fmt.Println("error:",err)
			},
		}).Do(time.Now().Add(5*time.Second))
	}
}

func uint16BE(v uint16) (b [2]byte) {
	b[0] = byte(v >> 8)
	b[1] = byte(v)
	return
}
func beUint16(b [2]byte) (v uint16) {
	v = binary.BigEndian.Uint16(b[:])
	return
}