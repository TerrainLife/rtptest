package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/wernerd/GoRTP/src/net/rtp"
)

const (
	payloadSize = 160
)

var patternsMap = map[string][]byte{
	"1000Hz": []byte{0xD5, 0x23, 0x2A, 0x23, 0xD5, 0xA3, 0xAA, 0xA3},
	"500Hz":  []byte{0xD5, 0x8D, 0xB3, 0xB8, 0xA5, 0xB8, 0xB3, 0x8D, 0x55, 0x0D, 0x33, 0x38, 0x25, 0x38, 0x33, 0x0D},
}

type host struct {
	ip   string
	port int
}

type runFlags struct {
	genTone string
	rdFile  string
	wrFile  string
	count   int
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

func receiveFn(rtpSession *rtp.Session, done <-chan struct{}, wg *sync.WaitGroup, params runFlags) {
	defer wg.Done()

	var wrFile *os.File
	if params.wrFile != "" {
		var err error
		wrFile, err = os.Create(params.wrFile)
		if err != nil {
			log.Fatalf("Error open file %s : %s\n", params.wrFile, err.Error())
		}
		defer wrFile.Close()
	}

	// Create and store the data receive channel.
	dataReceiver := rtpSession.CreateDataReceiveChan()
	var cnt int

	for {
		select {
		case rp := <-dataReceiver: // just get a packet - maybe we add some tests later
			if params.wrFile != "" {
				wrFile.Write(rp.Payload())
			}
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

func senderFn(rtpSession *rtp.Session, done <-chan struct{}, wg *sync.WaitGroup, params runFlags) {
	defer wg.Done()

	var rdFile *os.File
	if params.rdFile != "" {
		var err error
		rdFile, err = os.Open(params.rdFile)
		if err != nil {
			log.Fatalf("Error open file %s : %s\n", params.rdFile, err.Error())
		}
		defer rdFile.Close()

		// check on file size greater than minimal packet payload size
		stat, err := rdFile.Stat()
		if err != nil {
			log.Fatalf("Error checking read file size %s : %s\n", params.rdFile, err.Error())
		}
		if stat.Size() < payloadSize {
			log.Fatalln("Error: read file size must be gerater than ", payloadSize)
		}
	}

	ticker := time.NewTicker(20 * time.Millisecond)
	var cnt int
	stamp := uint32(0)

	payload := make([]byte, payloadSize)
	for {
		select {
		case <-ticker.C:
			rp := rtpSession.NewDataPacket(stamp)

			if params.rdFile != "" {
				n, err := rdFile.Read(payload)
				if err != nil {
					rdFile.Seek(0, 0)
					if err == io.EOF && n < payloadSize {
						rdFile.Read(payload[n:])
					}
				}
			} else if params.genTone != "" {
				tone, pres := patternsMap[params.genTone]
				if pres {
					payload = tone
				}
			}

			rp.SetPayload(payload[:])
			rtpSession.WriteData(rp)
			rp.FreePacket()
			if (cnt % 50) == 0 {
				fmt.Printf("Sent %d packets\n", cnt)
				//fmt.Println(string(payload[:]))
				//printBuf(payload)
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

func initRtpSession(wgmain *sync.WaitGroup, params runFlags, local host, remote ...host) {
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
	go receiveFn(rtpSession, done, &wg, params)

	fmt.Printf("Start RTP Session %s:%d ssrc=%x\n", local.ip, local.port, rtpSession.SsrcStreamOutForIndex(outStreamIdx).Ssrc())
	fmt.Printf("Remote hosts:\n")
	for _, remoteHost := range remote {
		fmt.Printf("%s:%d", remoteHost.ip, remoteHost.port)
	}
	fmt.Printf("\n")

	err = rtpSession.StartSession()
	if err != nil {
		log.Fatalln("Error start rtp session", err.Error())
	}

	wg.Add(1)
	go senderFn(rtpSession, done, &wg, params)

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

	var params runFlags
	flag.StringVar(&params.genTone, "freq", "1000Hz", "generated frequency")
	flag.StringVar(&params.rdFile, "rdfile", "", "read&send to ip data from this file (alaw, 8000Hz)")
	flag.StringVar(&params.wrFile, "wrfile", "", "received data writing to this file")
	flag.IntVar(&params.count, "cnt", 1, "number of rtp channels (local&remote port numbers increased by 2)")

	flag.Parse()

	var wgmain sync.WaitGroup
	for i := 0; i < params.count; i++ {
		wgmain.Add(1)
		go initRtpSession(&wgmain, params, local, remote)
		local.port += 2
		remote.port += 2
	}

	wgmain.Wait()
	fmt.Println("RTP test stop!")
}
