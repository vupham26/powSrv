package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
    "flag"

	"github.com/muXxer/powsrv"
    "github.com/shufps/pidiver/pidiver"
    "github.com/iotaledger/giota"
)

const (
	socketPath = "/tmp/powSrv.sock"
)

// The flag package provides a default help printer via -h switch
var forceFlash *bool        =   flag.Bool("force-upload", false, "Force file upload to SPI flash")
var forceConfigure *bool    =   flag.Bool("force-configure", false, "Force to configure FPGA from SPI flash")
var configFile *string      =   flag.String("fpga-config", "output_file.rbf", "FPGA config file to upload to SPI flash")
var device *string          =   flag.String("device", "/dev/ttyACM0", "Device file for usb communication")
var useUSB *bool            =   flag.Bool("usbdiver", false, "Use USB instead of Pi-GPIO")


func main() {
    flag.Parse() // Scan the arguments list

    // initialize pidiver
	config := pidiver.PiDiverConfig{
		Device:         *device,
		ConfigFile:     *configFile,
		ForceFlash:     *forceFlash,
		ForceConfigure: *forceConfigure}

	var powFunc giota.PowFunc
	var err error
	if *useUSB {
		err = pidiver.InitUSBDiver(&config)
		powFunc = pidiver.PowUSBDiver
	} else {
		err = pidiver.InitPiDiver(&config)
		powFunc = pidiver.PowPiDiver
	}
	if err != nil {
		log.Fatal(err)
	}	
	
	powsrv.SetPowFunc(powFunc)
		
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
