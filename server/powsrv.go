package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/muXxer/powsrv"
)

const (
	socketPath = "/tmp/powSrv.sock"
)

func main() {
	// Servers should unlink the socket pathname prior to binding it.
	// https://troydhanson.github.io/network/Unix_domain_sockets.html
	syscall.Unlink(socketPath)

	log.Println("Starting powSrv...")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatal("Listen error:", err)
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	go func(ln net.Listener, c chan os.Signal) {
		sig := <-c
		log.Printf("Caught signal %s: powSrv shutting down.", sig)
		ln.Close()
		os.Exit(0)
	}(ln, sigc)

	log.Println("powSrv started. Waiting for connections...")
	for {
		fd, err := ln.Accept()
		if err != nil {
			log.Print("Accept error: ", err)
			continue
		}

		go powsrv.HandleClientConnection(fd)
	}
}
