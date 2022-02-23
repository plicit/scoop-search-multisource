package main

import (
	"log"
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
