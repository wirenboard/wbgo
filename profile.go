package wbgo

import (
	"flag"
	"fmt"
	// MQTT "github.com/contactless/org.eclipse.paho.mqtt.golang"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"os"
	"os/signal"
	"runtime/pprof"
	"syscall"
	"time"
)

var dieAfter = flag.Int("die", 0, "Die after specified number of seconds")
var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")

func getRusage() (userTime time.Duration, sysTime time.Duration) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		fmt.Fprintln(os.Stderr, "*** -cpuprofile: Getrusage() failed")
		return 0, 0
	}
	userTime = time.Duration(ru.Utime.Sec)*time.Second + time.Duration(ru.Utime.Usec)*time.Microsecond
	sysTime = time.Duration(ru.Stime.Sec)*time.Second + time.Duration(ru.Stime.Usec)*time.Microsecond
	return
}

// TBD: move to wbgo (together with flags)
// also, make it possible to disable gc there using flags
func profile(profFile string, dieAfter time.Duration, readyCh <-chan struct{}) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	go func() {
		if readyCh != nil {
			<-readyCh
		}
		start := time.Now()
		initialUserTime, initialSysTime := getRusage()
		//packetsInitiallySent, packetsInitiallyReceived := MQTT.GetStats()

		f, err := os.Create(profFile)
		if err != nil {
			Error.Fatalf("error creating profiling file: %s", err)
		}
		pprof.StartCPUProfile(f)

		// TBD: add timer
		var dieCh <-chan time.Time = nil
		if dieAfter > 0 {
			dieCh = time.After(dieAfter)
		}
		select {
		case <-ch:
		case <-dieCh:
		}
		newUserTime, newSysTime := getRusage()
		elapsed := time.Since(start)
		if elapsed == 0 {
			fmt.Fprintln(os.Stderr, "oops, no time elapsed?")
			return
		}
		elapsedUserTime := newUserTime - initialUserTime
		elapsedSysTime := newSysTime - initialSysTime
		elapsedCpuTime := elapsedUserTime + elapsedSysTime
		cpuLoadUser := float64(elapsedUserTime) * 100.0 / float64(elapsed)
		cpuLoadSys := float64(elapsedSysTime) * 100.0 / float64(elapsed)
		cpuLoad := float64(elapsedCpuTime) * 100.0 / float64(elapsed)

		/*
			newPacketsSent, newPacketsReceived := MQTT.GetStats()
			packetsSent := newPacketsSent - packetsInitiallySent
			packetsReceived := newPacketsReceived - packetsInitiallyReceived
			if packetsSent > 0 || packetsReceived > 0 {
				packetsSentPerSecond := float64(packetsSent) * float64(time.Second) /
					float64(elapsed)
				packetsReceivedPerSecond := float64(packetsReceived) * float64(time.Second) /
					float64(elapsed)
				packetsSentPerCpuSecond := float64(packetsSent) * float64(time.Second) /
					float64(elapsedCpuTime)
				packetsReceivedPerCpuSecond := float64(packetsReceived) * float64(time.Second) /
					float64(elapsedCpuTime)

				fmt.Printf("\n*** %d packets sent (%.2f per sec, %.2f per cpu sec), "+
					"%d packets received (%.2f per sec, %.2f per cpu sec)\n",
					packetsSent, packetsSentPerSecond, packetsSentPerCpuSecond,
					packetsReceived, packetsReceivedPerSecond, packetsReceivedPerCpuSecond)
			}
		*/

		fmt.Printf("*** %.2f seconds elapsed, %.2f user, %.2f sys\n",
			float64(elapsed)/float64(time.Second),
			float64(elapsedUserTime)/float64(time.Second),
			float64(elapsedSysTime)/float64(time.Second))
		fmt.Printf("*** %.2f%% CPU load, %.2f%% user, %.2f%% sys\n",
			cpuLoad, cpuLoadUser, cpuLoadSys)
		pprof.StopCPUProfile()
		os.Exit(130)
	}()
}

func MaybeInitProfiling(readyCh <-chan struct{}) {
	if *cpuprofile != "" {
		profile(*cpuprofile, time.Duration(*dieAfter)*time.Second, readyCh)
	}
}
