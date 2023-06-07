package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

func ReadAccess(filename string) string {
	ajson, err := os.Open("access.json")
	if err != nil {
		log.Fatal(err)
	}
	defer ajson.Close()

	data, err := io.ReadAll(ajson)
	if err != nil {
		log.Fatal(err)
	}

	var result struct{ Token string }
	err = json.Unmarshal(data, &result)
	if err != nil {
		log.Fatal(err)
	}
	return result.Token
}

func GetOpen(token string) []int {
	req, err := http.NewRequest(http.MethodGet, "https://git.niisi.ru/api/v4/projects/42/merge_requests?state=opened", nil)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("PRIVATE-TOKEN", token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Fatal("Status code is not OK")
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	var res []struct{ Iid int }
	err = json.Unmarshal(bodyBytes, &res)
	if err != nil {
		log.Fatal(err)
	}

	var result []int
	for _, mr := range res {
		result = append(result, mr.Iid)
	}
	return result
}

func main() {
	token := ReadAccess("access.json")
	fmt.Println(token)
	danglins := GetOpen(token)
	fmt.Println(danglins)

}
