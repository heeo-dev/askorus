// tsprobe runs the phone's Tailscale bridge on a desk computer, to try it
// without building the app (#57). It starts the node, prints its state
// until it is running (opening the login link is up to the person), says
// what it knows of a host, and then sends one GET request to it.
//
//	tsprobe -dir <folder for the node key> -name <device name> [-url http://askorus-pc.tailnet.ts.net:8790/v1/info] [-token T]
package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/heeo-dev/askorus/tsbridge"
)

func main() {
	dir := flag.String("dir", "", "folder that keeps the node key")
	name := flag.String("name", "askorus-probe", "device name on the tailnet")
	target := flag.String("url", "", "address to GET once the node is running")
	token := flag.String("token", "", "device token to send")
	wait := flag.Duration("wait", 5*time.Minute, "how long to wait for the node to run")
	madeUp := flag.Bool("madeup", false, "use the made-up interface list the phone uses")
	verbose := flag.Bool("v", false, "print the backend's log")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "tsprobe: -dir is needed")
		os.Exit(2)
	}
	if *madeUp {
		tsbridge.UseMadeUpInterfaces()
	}
	tsbridge.SetVerbose(*verbose)
	if err := tsbridge.Start(*dir, *name); err != nil {
		fmt.Fprintln(os.Stderr, "start:", err)
		os.Exit(1)
	}
	defer tsbridge.Stop()

	deadline := time.Now().Add(*wait)
	last := ""
	for {
		st, err := tsbridge.Status()
		if err != nil {
			st = "error: " + err.Error()
		}
		if st != last {
			fmt.Println(st)
			last = st
		}
		if strings.Contains(st, `"state":"Running"`) {
			break
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "tsprobe: not running in time")
			os.Exit(1)
		}
		time.Sleep(time.Second)
	}
	if *target == "" {
		return
	}
	if u, err := url.Parse(*target); err == nil {
		who, err := tsbridge.Peer(u.Hostname())
		fmt.Println("peer:", who, err)
	}
	headers := `{"Accept":"application/json"}`
	if *token != "" {
		headers = `{"Accept":"application/json","Authorization":"Bearer ` + *token + `"}`
	}
	for i := 0; i < 3; i++ {
		res, err := tsbridge.Fetch("GET", *target, headers, "", 20000)
		if err != nil {
			fmt.Println("fetch:", err)
			continue
		}
		body := res.Body
		if len(body) > 300 {
			body = body[:300] + "..."
		}
		fmt.Printf("%d in %d ms\n%s\n", res.Status, res.Ms, body)
	}
}
