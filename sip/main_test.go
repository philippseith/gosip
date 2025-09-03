package sip_test

import (
	"log"
	"testing"

	"github.com/spf13/viper"
)

var interfaceName string
var serverAddress string
var identifyNode [6]byte

func TestMain(m *testing.M) {
	viper.SetConfigFile("testdata/test_config.json")
	err := viper.ReadInConfig()
	if err != nil {
		log.Panic(err)
	}

	interfaceName = viper.GetString("interfaceName")
	serverAddress = viper.GetString("serverAddress")
	for i, v := range viper.GetIntSlice("identifyNode") {
		identifyNode[i] = byte(v)
	}

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	m.Run()
}
