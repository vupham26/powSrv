package main

import (
	"encoding/json"
	"errors"
	"fmt"
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
	flag.StringP("fpga.core", "f", "pidiver1.1.rbf", "Core/config file to upload to FPGA")
	flag.StringP("usb.device", "d", "/dev/ttyACM0", "Device file for usb communication")

	flag.StringP("pow.type", "t", "giota", "'giota', 'giota-go', giota-c', 'giota-c128', 'giota-sse', 'pidiver', 'usbdiver' or 'cyc1000'")
	flag.IntP("pow.maxMinWeightMagnitude", "m", 20, "Maximum Min-Weight-Magnitude (Difficulty for PoW)")

	var logLevel = flag.StringP("log.level", "l", "INFO", "'DEBUG', 'INFO', 'NOTICE', 'WARNING', 'ERROR' or 'CRITICAL'")

	flag.StringP("server.socketPath", "s", "/tmp/powSrv.sock", "Unix socket path of powSrv")

	config.BindPFlags(flag.CommandLine)

	var configPath = flag.StringP("config", "c", "", "Config file path")
	flag.Parse()

	logs.SetLogLevel(*logLevel)

	// Bind environment vars
	replacer := strings.NewReplacer(".", "_")
	config.SetEnvPrefix("POWSRV")
	config.SetEnvKeyReplacer(replacer)
	config.AutomaticEnv()

	// Load config
	if len(*configPath) > 0 {
		logs.Log.Infof("Loading config from: %s", *configPath)
		config.SetConfigFile(*configPath)
		err := config.ReadInConfig()
		if err != nil {
			logs.Log.Fatalf("Config could not be loaded from: %s", *configPath)
		}
	}

	return config
}

func init() {
	logs.Setup()
	config = loadConfig()
	logs.SetLogLevel(config.GetString("log.level"))

	cfg, _ := json.MarshalIndent(config.AllSettings(), "", "  ")
	logs.Log.Debugf("Following settings loaded: \n %+v", string(cfg))
}

func main() {
	flag.Parse() // Scan the arguments list

	var powFunc giota.PowFunc
	var powType string
	var powVersion string

	switch strings.ToLower(config.GetString("pow.type")) {

	case "giota":
		powType, powFunc = giota.GetBestPoW()
		powVersion = ""

	case "giota-go":
		powFunc = giota.PowGo
		powType = "gIOTA-Go"
		powVersion = ""

	case "giota-c":
		powFunc = giota.PowC
		powType = "gIOTA-PowC"
		powVersion = ""

	case "giota-c128":
		powFunc = giota.PowC128
		powType = "gIOTA-PowC128"
		powVersion = ""

	case "giota-sse":
		powFunc = giota.PowSSE
		powType = "gIOTA-PowSSE"
		powVersion = ""

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
		powVersion = "not implemented yet"
		/*
			powVersion, err := pidiver.GetFPGAVersion()
			if err != nil {
				log.Fatal(err)
			}
		*/
		powFunc = pidiver.PowPiDiver
		powType = "PiDiver"

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
		powVersion = "not implemented yet"
		/*
			powVersion, err := pidiver.GetFPGAVersion()
			if err != nil {
				log.Fatal(err)
			}
		*/
		powFunc = pidiver.PowUSBDiver
		powType = "USBDiver"

	case "cyc1000":
		log.Fatal(errors.New("cyc1000 not implemented yet"))
		powType = "CYC1000"
		powVersion = "not implemented yet"
		/*
			powVersion, err := pidiver.GetFPGAVersion()
			if err != nil {
				log.Fatal(err)
			}
		*/

	default:
		log.Fatal(errors.New("Unknown POW type"))

	}

	powsrv.SetPowFunc(powFunc)

	// Servers should unlink the socket pathname prior to binding it.
	// https://troydhanson.github.io/network/Unix_domain_sockets.html
	syscall.Unlink(config.GetString("server.socketPath"))

	log.Println("Starting powSrv...")
	ln, err := net.Listen("unix", config.GetString("server.socketPath"))
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

	log.Println(fmt.Sprintf("Using POW type: %v", powType))
	log.Println("powSrv started. Waiting for connections...")
	for {
		fd, err := ln.Accept()
		if err != nil {
			log.Print("Accept error: ", err)
			continue
		}

		go powsrv.HandleClientConnection(fd, config, powType, powVersion)
	}
}
