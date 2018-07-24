package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/iotaledger/giota"
	"github.com/muxxer/powsrv"
	"github.com/shufps/pidiver/pidiver"
	"github.com/shufps/pidiver/raspberry"
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/muxxer/ftdiver"
	"github.com/muxxer/powsrv/logs"
)

type PowConfigDevice struct {
	Type    string
	Network string
	Address string
	Core    string
	Device  string
}

type PowConfig struct {
	Devices []PowConfigDevice
}

var config *viper.Viper
var powConfig *PowConfig

/*
PRECEDENCE (Higher number overrides the others):
1. default
2. key/value store
3. config
4. env
5. flag
6. explicit call to Set
*/
func loadConfig() (*viper.Viper, *PowConfig) {
	// Setup Viper
	var config = viper.New()
	var powConfig *PowConfig

	// Get command line arguments
	// The flag package provides a default help printer via -h switch
	flag.IntP("pow.maxMinWeightMagnitude", "m", 20, "Maximum Min-Weight-Magnitude (Difficulty for PoW)")

	var logLevel = flag.StringP("log.level", "l", "INFO", "'DEBUG', 'INFO', 'NOTICE', 'WARNING', 'ERROR' or 'CRITICAL'")

	flag.StringP("server.socketPath", "s", "/tmp/powSrv.sock", "Unix socket path of powSrv")

	config.BindPFlags(flag.CommandLine)

	var configPath = flag.StringP("config", "c", "powsrv.config.json", "Config file path")
	flag.Parse()
	logs.SetLogLevel(*logLevel)

	// Bind environment vars
	replacer := strings.NewReplacer(".", "_")
	config.SetEnvPrefix("POWSRV")
	config.SetEnvKeyReplacer(replacer)
	config.AutomaticEnv()

	// Load config
	if len(*configPath) > 0 {
		_, err := os.Stat(*configPath)
		if !flag.CommandLine.Changed("config") && os.IsNotExist(err) {
			// Standard config file not found => skip
			logs.Log.Info("Standard config file not found. Loading default settings.")
			powType, _ := giota.GetBestPoW()
			powDevice := PowConfigDevice{Type: powType}
			powConfig := PowConfig{Devices: []PowConfigDevice{powDevice}}
			return config, &powConfig
		}

		logs.Log.Infof("Loading config from: %s", *configPath)
		config.SetConfigFile(*configPath)
		err = config.ReadInConfig()
		if err != nil {
			logs.Log.Fatalf("Config could not be loaded from: %s, %v", *configPath, err.Error())
		}
	}

	err := config.UnmarshalKey("pow", &powConfig)
	if err != nil {
		logs.Log.Fatalf("Config could not be loaded from: %s, %v", *configPath, err.Error())
	}

	return config, powConfig
}

func init() {
	logs.Setup()
	config, powConfig = loadConfig()
	logs.SetLogLevel(config.GetString("log.level"))

	cfg, _ := json.MarshalIndent(config.AllSettings(), "", "  ")
	logs.Log.Debugf("Following settings loaded: \n %+v", string(cfg))
}

func main() {
	flag.Parse() // Scan the arguments list

	var powDevices []powsrv.PowDevice
	var err error

	for _, device := range powConfig.Devices {
		var powFunc giota.PowFunc
		var powType string
		var powVersion string

		switch strings.ToLower(device.Type) {

		case "giota":
			powType, powFunc = giota.GetBestPoW()
			powVersion = ""

		case "giota-go":
			powFunc = giota.PowGo
			powType = "gIOTA-Go"

		case "giota-cl":
			powFunc, err = giota.GetPowFunc("PowCL")
			if err == nil {
				powType = "gIOTA-PowCL"
			} else {
				powType, powFunc = giota.GetBestPoW()
				logs.Log.Infof("POW type '%s' not available. Using '%s' instead", "PowCL", powType)
			}

		case "giota-sse":
			powFunc, err = giota.GetPowFunc("PowSSE")
			if err == nil {
				powType = "gIOTA-PowSSE"
			} else {
				powType, powFunc = giota.GetBestPoW()
				logs.Log.Infof("POW type '%s' not available. Using '%s' instead", "PowSSE", powType)
			}

		case "giota-carm64":
			powFunc, err = giota.GetPowFunc("PowCARM64")
			if err == nil {
				powType = "gIOTA-PowCARM64"
			} else {
				powType, powFunc = giota.GetBestPoW()
				logs.Log.Infof("POW type '%s' not available. Using '%s' instead", "PowCARM64", powType)
			}

		case "giota-c128":
			powFunc, err = giota.GetPowFunc("PowC128")
			if err == nil {
				powType = "gIOTA-PowC128"
			} else {
				powType, powFunc = giota.GetBestPoW()
				logs.Log.Infof("POW type '%s' not available. Using '%s' instead", "PowC128", powType)
			}

		case "giota-c":
			powFunc, err = giota.GetPowFunc("PowC")
			if err == nil {
				powType = "gIOTA-PowC"
			} else {
				powType, powFunc = giota.GetBestPoW()
				logs.Log.Infof("POW type '%s' not available. Using '%s' instead", "PowC", powType)
			}

		case "pidiver":
			piconfig := pidiver.PiDiverConfig{
				Device:         "",
				ConfigFile:     device.Core,
				ForceFlash:     false,
				ForceConfigure: false}

			// initialize pidiver
			llStruct := raspberry.GetLowLevel()
			err := pidiver.InitPiDiver(&llStruct, &piconfig)
			if err != nil {
				logs.Log.Fatal(err)
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
				Device:         device.Device,
				ConfigFile:     device.Core,
				ForceFlash:     false,
				ForceConfigure: false}

			// initialize pidiver
			err := pidiver.InitUSBDiver(&piconfig)
			if err != nil {
				logs.Log.Fatal(err)
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

		case "ftdiver":
			piconfig := pidiver.PiDiverConfig{
				Device:         "",
				ConfigFile:     "",
				ForceFlash:     false,
				ForceConfigure: false}

			// initialize pidiver
			llStruct := ftdiver.GetLowLevel()
			err := pidiver.InitPiDiver(&llStruct, &piconfig)
			if err != nil {
				logs.Log.Fatal(err)
			}
			powVersion = "not implemented yet"
			/*
				powVersion, err := pidiver.GetFPGAVersion()
				if err != nil {
					logs.Log.Fatal(err)
				}
			*/
			powFunc = pidiver.PowPiDiver
			powType = "ftdiver"

		case "powsrv":
			powClient := powsrv.PowClient{Network: device.Network, Address: device.Address, WriteTimeOutMs: 500, ReadTimeOutMs: 5000}
			powClient.Init()
			_, powType, powVersion, err = powClient.GetPowInfo()
			if err != nil {
				logs.Log.Fatal(err.Error())
			}
			powFunc = powClient.PowFunc

		default:
			logs.Log.Fatal("Unknown POW type")
		}

		powDevices = append(powDevices, powsrv.PowDevice{PowType: powType, PowVersion: powVersion, PowFunc: powFunc, PowMutex: &sync.Mutex{}})
	}
	// Servers should unlink the socket pathname prior to binding it.
	// https://troydhanson.github.io/network/Unix_domain_sockets.html
	syscall.Unlink(config.GetString("server.socketPath"))

	logs.Log.Info("Starting powSrv...")
	ln, err := net.Listen("unix", config.GetString("server.socketPath"))
	if err != nil {
		logs.Log.Fatal("Listen error:", err)
	}

	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt, syscall.SIGTERM)
	go func(ln net.Listener, c chan os.Signal) {
		sig := <-c
		logs.Log.Infof("Caught signal %s: powSrv shutting down.", sig)
		ln.Close()
		os.Exit(0)
	}(ln, sigc)

	logs.Log.Info("powSrv started. Waiting for connections...")
	logs.Log.Infof("Listening for connections on \"%v\"", config.GetString("server.socketPath"))

	for i, dev := range powDevices {
		logs.Log.Infof("POW Device %d: Using POW type: %v", i, dev.PowType)
	}

	powTypes := ""
	powVersions := ""
	for i, dev := range powDevices {
		powTypes += fmt.Sprintf("[%d] %v, ", i, dev.PowType)
		powVersions += fmt.Sprintf("[%d] %v, ", i, dev.PowVersion)
	}

	for {
		fd, err := ln.Accept()
		if err != nil {
			logs.Log.Info("Accept error: ", err)
			continue
		} else {
			logs.Log.Debugf("New connection accepted from \"%v\"", fd.RemoteAddr)
		}

		// Only one client connection at a time (ToDo: could be improved to handle several)
		powsrv.HandleClientConnection(fd, config, powDevices, powTypes, powVersions)
	}
}
