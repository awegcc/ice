package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/gortc/stun"
)

const (
	server1       = "aweg.cc:3478"
	udp           = "udp4"
	pingMsg       = "ping"
	pongMsg       = "pong"
	timeoutMillis = 3000
)

func main() {
	server2 := flag.String("server", fmt.Sprintf("chare.cc:20022"), "Stun server address")
	logfile := flag.String("logfile", "/tmp/stun.log", "logfile")
	flag.Parse()
	if *logfile != "" {
		fd, err := os.OpenFile(*logfile, os.O_CREATE|os.O_CREATE|os.O_RDWR, 0644)
		if err != nil {
			log.Fatal("Create file : ", err)
		}
		log.SetOutput(fd)
	}

	srvAddr1, err := net.ResolveUDPAddr(udp, server1)
	if err != nil {
		log.Fatalln("resolve serveraddr:", err)
	}
	srvAddr2, err := net.ResolveUDPAddr(udp, *server2)
	if err != nil {
		log.Fatalln("resolve serveraddr:", err)
	}
	fmt.Println("server1: ", srvAddr1)
	fmt.Println("server2: ", srvAddr2)

	conn, err := net.ListenUDP(udp, nil)
	if err != nil {
		log.Fatalln("dial:", err)
	}

	defer conn.Close()
	fmt.Printf("Listening address: %s\n", conn.LocalAddr())
	log.Printf("Listening address: %s\n", conn.LocalAddr())

	var publicAddr1, publicAddr2 stun.XORMappedAddress
	var peerAddr *net.UDPAddr

	messageChan := listen(conn)
	var peerAddrChan <-chan string

	keepalive := time.Tick(timeoutMillis * time.Millisecond)
	keepaliveMsg := pingMsg

	var quitChan <-chan time.Time

	gotPong := false
	sentPong := false

	for {
		select {
		case message, ok := <-messageChan:
			if !ok {
				return
			}

			switch {
			case string(message) == pingMsg:
				fmt.Printf("Received %s message.\n", pingMsg)
				log.Printf("Received %s message.", pingMsg)
				keepaliveMsg = pongMsg

			case string(message) == pongMsg:
				if !gotPong {
					fmt.Printf("Received %s message.\n", pongMsg)
					log.Printf("Received %s message.", pongMsg)
				}

				// One client may skip sending ping if it receives
				// a ping message before knowning the peer address.
				keepaliveMsg = pongMsg

				gotPong = true

			case stun.IsMessage(message):
				m := new(stun.Message)
				m.Raw = message
				err := m.Decode()
				if err != nil {
					log.Println("decode:", err)
					break
				}
				var xorAddr stun.XORMappedAddress
				if err := xorAddr.GetFrom(m); err != nil {
					log.Println("getFrom:", err)
					break
				}

				if publicAddr1.String() != xorAddr.String() && publicAddr2.String() != xorAddr.String() {
					fmt.Printf("My public address: %s\n", xorAddr)
					log.Printf("My public address: %s\n", xorAddr)
					publicAddr1 = xorAddr
					publicAddr2 = xorAddr

					peerAddrChan = getPeerAddr()
				}

			default:
				log.Fatalln("unknown message", message)
			}

		case peerStr := <-peerAddrChan:
			peerAddr, err = net.ResolveUDPAddr(udp, peerStr)
			if err != nil {
				fmt.Println("resolve peeraddr error :", err)
				log.Fatalln("resolve peeraddr error :", err)
			}
			fmt.Printf("resolve peeraddr: %v\n", peerAddr)
			log.Printf("resolve peeraddr: %v\n", peerAddr)

		case <-keepalive:
			// Keep NAT binding alive using STUN server or the peer once it's known
			if peerAddr == nil {
				//log.Printf("peerAddr is nil , send bind request to %v\n", srvAddr2)
				err = sendBindingRequest(conn, srvAddr1)
				if err != nil {
					fmt.Println("sendBindingRequest error: ", err)
				}
				err = sendBindingRequest(conn, srvAddr2)
				if err != nil {
					fmt.Println("sendBindingRequest error: ", err)
				}
			} else {
				log.Printf("send keepalive %v to %v\n", keepaliveMsg, peerAddr)
				err = sendStr(keepaliveMsg, conn, peerAddr)
				if keepaliveMsg == pongMsg {
					sentPong = true
				}
			}

			if err != nil {
				log.Fatalln("keepalive:", err)
			}

		case <-quitChan:
			log.Println("quit !!")
			conn.Close()
		}

		if quitChan == nil && gotPong && sentPong {
			log.Println("Success! Quitting in two seconds.")
			quitChan = time.After(8 * time.Second)
		}
	}
}

func getPeerAddr() <-chan string {
	result := make(chan string)

	go func() {
		reader := bufio.NewReader(os.Stdin)
		fmt.Printf("Enter remote peer address:")
		peer, _ := reader.ReadString('\n')
		result <- strings.Trim(peer, " \r\n")
	}()

	return result
}

func listen(conn *net.UDPConn) <-chan []byte {
	messages := make(chan []byte)
	go func() {
		for {
			buf := make([]byte, 1024)

			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				close(messages)
				return
			}
			log.Printf("read from addr: %v\n", addr)
			buf = buf[:n]

			messages <- buf
		}
	}()
	return messages
}

func sendBindingRequest(conn *net.UDPConn, addr *net.UDPAddr) error {
	m := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	err := send(m.Raw, conn, addr)
	if err != nil {
		return fmt.Errorf("binding: %v", err)
	}

	return nil
}

func send(msg []byte, conn *net.UDPConn, addr *net.UDPAddr) error {
	_, err := conn.WriteToUDP(msg, addr)
	if err != nil {
		return fmt.Errorf("send: %v", err)
	}

	return nil
}

func sendStr(msg string, conn *net.UDPConn, addr *net.UDPAddr) error {
	return send([]byte(msg), conn, addr)
}
