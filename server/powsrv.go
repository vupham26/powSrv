package main

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/iotaledger/giota"
	"github.com/muXxer/powsrv"
	"github.com/shufps/pidiver/pidiver"
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"

	"./logs"
)

var config *viper.Viper

/*
PRECEDENCE (Higher number overrides the others):
1. default
2. key/value store
3. config
4. env
5. flag
6. explicit call to Set
*/
func loadConfig() *viper.Viper {
	// Setup Viper
	var config = viper.New()

	// Get command line arguments
	// The flag package provides a default help printer via -h switch
	flag.String("fpga.core", "pidiver1.1.rbf", "Core/config file to upload to FPGA")
	flag.StringP("usb.device", "d", "/dev/ttyACM0", "Device file for usb communication")

	flag.StringP("pow.type", "t", "giota", "'giota', 'giota-go', giota-c', 'giota-c128', 'giota-sse', 'pidiver', 'usbdiver' or 'cyc1000'")
	flag.IntP("pow.maxMinWeightMagnitude", "mwm", 20, "Maximum Min-Weight-Magnitude (Difficulty for PoW)")

	flag.StringP("log.level", "ll", "INFO", "'DEBUG', 'INFO', 'NOTICE', 'WARNING', 'ERROR' or 'CRITICAL'")

	flag.StringP("config", "c", "", "Config file path")
	flag.StringP("socket-path", "s", "/tmp/powSrv.sock", "Unix socket path of powSrv")

	flag.Parse()
	config.BindPFlags(flag.CommandLine)

	// Bind environment vars
	replacer := strings.NewReplacer(".", "_")
	config.SetEnvPrefix("POWSRV")
	config.SetEnvKeyReplacer(replacer)
	config.AutomaticEnv()

	// Load config
	var configPath = config.GetString("config")
	if len(configPath) > 0 {
		logs.Log.Infof("Loading config from: %s", configPath)
		config.SetConfigFile(configPath)
		err := config.ReadInConfig()
		if err != nil {
			logs.Log.Fatalf("Config could not be loaded from: %s", configPath)
		}
	}

	return config
}

func init() {
	logs.Setup()
	config = loadConfig()
	logs.SetConfig(config)

	cfg, _ := json.MarshalIndent(config.AllSettings(), "", "  ")
	logs.Log.Debugf("Following settings loaded: \n %+v", string(cfg))
}

func main() {
	flag.Parse() // Scan the arguments list

	var powFunc giota.PowFunc

	switch strings.ToLower(config.GetString("pow.type")) {

	case "giota":
		_, powFunc = giota.GetBestPoW()

	case "giota-go":
		powFunc = giota.PowGo

	case "giota-c":
		powFunc = giota.PowC

	case "giota-c128":
		powFunc = giota.PowC128

	case "giota-sse":
		powFunc = giota.PowSSE

	case "pidiver":
		piconfig := pidiver.PiDiverConfig{
			Device:         config.GetString("fpga.device"),
			ConfigFile:     config.GetString("fpga.core"),
			ForceFlash:     false,
			ForceConfigure: false}

		// initialize pidiver
		err := pidiver.InitPiDiver(&piconfig)
		if err != nil {
			log.Fatal(err)
		}

		powFunc = pidiver.PowPiDiver

	case "usbdiver":
		piconfig := pidiver.PiDiverConfig{
			Device:         config.GetString("fpga.device"),
			ConfigFile:     config.GetString("fpga.config"),
			ForceFlash:     config.GetBool("fpga.force-upload"),
			ForceConfigure: config.GetBool("fpga.force-configure")}

		// initialize pidiver
		err := pidiver.InitUSBDiver(&piconfig)
		if err != nil {
			log.Fatal(err)
		}

		powFunc = pidiver.PowUSBDiver

	case "cyc1000":
		log.Fatal(errors.New("cyc1000 not implemented yet"))

	default:
		log.Fatal(errors.New("Unknown POW type"))

	}

	powsrv.SetPowFunc(powFunc)

	// Servers should unlink the socket pathname prior to binding it.
	// https://troydhanson.github.io/network/Unix_domain_sockets.html
	syscall.Unlink(config.GetString("socket-path"))

	log.Println("Starting powSrv...")
	ln, err := net.Listen("unix", config.GetString("socket-path"))
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

		go powsrv.HandleClientConnection(fd, config)
	}
}
