package main

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/wernerd/GoRTP/src/net/rtp"
)

const (
	payloadSize = 160
)

var patternsMap = map[string][]byte{
	"1000Hz": []byte{0xD5, 0x23, 0x2A, 0x23, 0xD5, 0xA3, 0xAA, 0xA3},
}

var localPort = 20002
var local, _ = net.ResolveIPAddr("ip", "172.20.32.3")

var remotePort = 10000
var remote, _ = net.ResolveIPAddr("ip", "172.20.32.200")

var localZone = ""
var remoteZone = ""

var rtpSession *rtp.Session

var ssrc uint32 = 0xdeadbeef

func receiveFn(done <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	// Create and store the data receive channel.
	dataReceiver := rtpSession.CreateDataReceiveChan()
	var cnt int

	for {
		select {
		case rp := <-dataReceiver: // just get a packet - maybe we add some tests later
			if (cnt % 50) == 0 {
				fmt.Printf("Remote receiver got %d packets\n", cnt)
			}
			cnt++
			rp.FreePacket()
		case <-done:
			return
		}
	}

}

func fillPatternMap() {
	for key, pattern := range patternsMap {
		var tmp []byte
		for i := 0; i < payloadSize/len(pattern); i++ {
			tmp = append(tmp, pattern...)
		}

		patternsMap[key] = tmp

		printBuf(patternsMap[key])
	}
}

func senderFn(done <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(20 * time.Millisecond)
	var cnt int
	stamp := uint32(0)
	for {
		select {
		case <-ticker.C:
			rp := rtpSession.NewDataPacket(stamp)

			rp.SetPayload(patternsMap["1000Hz"][:])
			rtpSession.WriteData(rp)
			rp.FreePacket()
			if (cnt % 50) == 0 {
				fmt.Printf("Sent %d packets\n", cnt)

				printBuf(patternsMap["1000Hz"])
			}
			cnt++
			stamp += 160

		case <-done:
			return
		}
	}
}

func printBuf(buf []byte) {
	for _, n := range buf {
		fmt.Printf("%x ", n)
	}
	fmt.Printf("\n")
	fmt.Println("buf len=%u", len(buf))
}

func main() {
	fmt.Println("RTP test start!")

	fillPatternMap()

	var wg sync.WaitGroup
	done := make(chan struct{}, 2)
	defer close(done)

	// Create a UDP transport with "local" address and use this for a "local" RTP session
	// The RTP session uses the transport to receive and send RTP packets to the remote peer.
	transport, _ := rtp.NewTransportUDP(local, localPort, localZone)

	// TransportUDP implements TransportWrite and TransportRecv interfaces thus
	// use it to initialize the Session for both interfaces.
	rtpSession = rtp.NewSession(transport, transport)

	// Add address of a remote peer (participant)
	rtpSession.AddRemote(&rtp.Address{remote.IP, remotePort, remotePort + 1, remoteZone})

	// Create a media stream.
	// The SSRC identifies the stream. Each stream has its own sequence number and other
	// context. A RTP session can have several RTP stream for example to send several
	// streams of the same media.
	//
	outStreamIdx, _ := rtpSession.NewSsrcStreamOut(&rtp.Address{local.IP, localPort, localPort + 1, localZone}, ssrc, 1)
	rtpSession.SsrcStreamOutForIndex(outStreamIdx).SetPayloadType(8)

	wg.Add(1)
	go receiveFn(done, &wg)

	rtpSession.StartSession()

	wg.Add(1)
	go senderFn(done, &wg)

	time.Sleep(120e9)

	done <- struct{}{}
	done <- struct{}{}
	wg.Wait()

	rtpSession.CloseSession()
	fmt.Println("RTP test stop!")
}
