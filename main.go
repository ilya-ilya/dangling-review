package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func ReadAccess(filename string) (string, error) {
	ajson, err := os.Open("access.json")
	if err != nil {
		return "", err
	}
	defer ajson.Close()

	data, err := io.ReadAll(ajson)
	if err != nil {
		return "", err
	}

	var result struct{ Token string }
	err = json.Unmarshal(data, &result)
	if err != nil {
		return "", err
	}
	return result.Token, nil
}

func GetOpen(token string) ([]int, error) {
	req, err := http.NewRequest(http.MethodGet, "https://git.niisi.ru/api/v4/projects/42/merge_requests?state=opened", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		log.Fatal("Status code is not OK")
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var res []struct{ Iid int }
	err = json.Unmarshal(bodyBytes, &res)
	if err != nil {
		return nil, err
	}

	var result []int
	for _, mr := range res {
		result = append(result, mr.Iid)
	}
	return result, nil
}

func KubeConnect() error {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		return err
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	pods, err := clientset.CoreV1().Pods("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	fmt.Printf("There are %d pods in the cluster\n", len(pods.Items))
	return nil
}

func main() {
	token, err := ReadAccess("access.json")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(token)

	danglins, err := GetOpen(token)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(danglins)

	if err = KubeConnect(); err != nil {
		log.Fatal(err)
	}
}
