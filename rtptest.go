package main

import (
	"flag"
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

type host struct {
	ip   string
	port int
}

func fillPatternMap() {
	for key, pattern := range patternsMap {
		var tmp []byte
		for i := 0; i < payloadSize/len(pattern); i++ {
			tmp = append(tmp, pattern...)
		}
		patternsMap[key] = tmp
	}
}

func receiveFn(rtpSession *rtp.Session, done <-chan struct{}, wg *sync.WaitGroup) {
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

func senderFn(rtpSession *rtp.Session, done <-chan struct{}, wg *sync.WaitGroup) {
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

func initRtpSession(wgmain *sync.WaitGroup, local host, remote ...host) {
	defer wgmain.Done()

	var wg sync.WaitGroup
	done := make(chan struct{}, 2)
	defer close(done)

	localHost, err := net.ResolveIPAddr("ip", local.ip)
	if err != nil {
		fmt.Printf("Resolve Local address %s FAIL:%s\n", local.ip, err.Error())
		return
	}

	// Create a UDP transport with "local" address and use this for a "local" RTP session
	// The RTP session uses the transport to receive and send RTP packets to the remote peer.
	transport, err := rtp.NewTransportUDP(localHost, local.port, "")
	if err != nil {
		fmt.Printf("Can not create %s:%d Transport layer:%s\n", local.ip, local.port, err.Error())
		return
	}

	// TransportUDP implements TransportWrite and TransportRecv interfaces thus
	// use it to initialize the Session for both interfaces.
	rtpSession := rtp.NewSession(transport, transport)

	for _, remoteHost := range remote {
		remoteHostConverted, err := net.ResolveIPAddr("ip", remoteHost.ip)
		if err != nil {
			fmt.Printf("Resolve Remote address %s FAIL:%s\n", remoteHost.ip, err.Error())
			continue
		}
		// Add address of a remote peer (participant)
		rtpSession.AddRemote(&rtp.Address{remoteHostConverted.IP, remoteHost.port, remoteHost.port + 1, ""})
	}

	// Create a media stream.
	// The SSRC identifies the stream. Each stream has its own sequence number and other
	// context. A RTP session can have several RTP stream for example to send several
	// streams of the same media.
	//
	outStreamIdx, _ := rtpSession.NewSsrcStreamOut(&rtp.Address{localHost.IP, local.port, local.port + 1, ""}, 0, 0)
	/* 	if err != nil {
		fmt.Printf("Can not create %s:%d ssrc stream out:%s\n", local.ip, local.port, err.Error())
		return
	} */
	rtpSession.SsrcStreamOutForIndex(outStreamIdx).SetPayloadType(8)

	wg.Add(1)
	go receiveFn(rtpSession, done, &wg)

	fmt.Printf("Start RTP Session %s:%d ssrc=%x\n", local.ip, local.port, rtpSession.SsrcStreamOutForIndex(outStreamIdx).Ssrc())
	fmt.Printf("Remote hosts:\n")
	for _, remoteHost := range remote {
		fmt.Printf("%s:%d", remoteHost.ip, remoteHost.port)
	}
	fmt.Printf("\n")

	rtpSession.StartSession()

	wg.Add(1)
	go senderFn(rtpSession, done, &wg)

	time.Sleep(30 * time.Minute)

	done <- struct{}{}
	done <- struct{}{}
	wg.Wait()

	rtpSession.CloseSession()
}

func main() {
	fmt.Println("RTP test start!")

	fillPatternMap()

	var local host
	flag.StringVar(&local.ip, "locip", "127.0.0.1", "local ip address")
	flag.IntVar(&local.port, "locport", 20000, "local port")

	var remote host
	flag.StringVar(&remote.ip, "remip", "127.0.0.1", "remote ip address")
	flag.IntVar(&remote.port, "remport", 10000, "remote port")

	//tone := flag.String("freq", "1000Hz", "generated frequency")

	flag.Parse()

	var wgmain sync.WaitGroup

	wgmain.Add(1)
	go initRtpSession(&wgmain, local, remote)

	wgmain.Wait()
	fmt.Println("RTP test stop!")
}
