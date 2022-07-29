package main

import (
	"fmt"
	"log"
	"os"
	"time"
	// "encoding/json"
)

func checkWith(err error, msg string) {
	if err != nil {
		log.Fatal(msg, " - ", err)
	}
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// func pp(data interface{}) {
// 	pretty, _ := json.MarshalIndent(data, "", " ")
// 	fmt.Println(string(pretty))
// }

// func stringInSlice(a string, list []string) bool {
// 	for _, b := range list {
// 		if b == a {
// 			return true
// 		}
// 	}
// 	return false
// }

func MinInt(x, y int) int {
	if x < y {
		return x
	}
	return y
}

func MaxInt(x, y int) int {
	if x > y {
		return x
	}
	return y
}

func fmtDuration(dur time.Duration) string {
	//dur = dur.Round(time.Second)
	Day := 24 * time.Hour
	d := dur / Day
	drem := dur % Day
	h := drem / time.Hour
	hrem := drem % time.Hour
	m := hrem / time.Minute
	//mrem := hrem % time.Minute
	//s := mrem / time.Second
	//str := ""
	if 24 < h {
		//return fmt.Sprintf("%dd%dh", d, h)
		return fmt.Sprintf("%dd", d) // day
	}
	if 0 < h {
		return fmt.Sprintf("%dh", h) // hour
	}
	//if 0 < m {
	return fmt.Sprintf("%dm", m) // minutes
}

func FirstPathThatExists(paths []string) string {
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil { //!errors.Is(err, os.ErrNotExist) {
			return path
		}
	}
	return ""
}
