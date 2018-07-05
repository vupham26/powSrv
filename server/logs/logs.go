package logs

import (
	"os"

	"github.com/op/go-logging"
	"github.com/spf13/viper"
)

var LOG_FORMAT = "%{color}[%{level:.4s}] %{time:15:04:05.000000} %{id:06x} [%{shortpkg}] %{longfunc} -> %{color:reset}%{message}"
var Log = logging.MustGetLogger("powSrv")

func Setup() {
	backend1 := logging.NewLogBackend(os.Stdout, "", 0)
	logging.SetFormatter(logging.MustStringFormatter(LOG_FORMAT))
	logging.SetBackend(backend1)
}

func SetConfig(config *viper.Viper) {
	level, err := logging.LogLevel(config.GetString("log.level"))
	if err == nil {
		logging.SetLevel(level, "powSrv")
	} else {
		Log.Warningf("Could not set log level to %v: %v", config.GetString("log.level"), err)
		Log.Warning("Using default log level")
	}
}
