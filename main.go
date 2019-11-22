package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime/trace"
	"syscall"

	"github.com/baetyl/baetyl-broker/utils"
	"github.com/baetyl/baetyl-broker/utils/log"
	_ "github.com/mattn/go-sqlite3"
)

func main() {

	// go tool pprof http://localhost:6060/debug/pprof/profile
	go func() {
		err := http.ListenAndServe("localhost:6060", nil)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Start profile failed: ", err.Error())
			return
		}
	}()

	f, err := os.Create("trace.out")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	err = trace.Start(f)
	if err != nil {
		panic(err)
	}
	defer trace.Stop()

	// baetyl.Run(func(ctx baetyl.Context) error {
	var cfg config
	utils.SetDefaults(&cfg)
	b, err := newBroker(cfg)
	if err != nil {
		log.Fatal("failed to create broker", err)
	}
	defer b.close()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	signal.Ignore(syscall.SIGPIPE)
	<-sig
	// })
}
