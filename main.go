package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/exp/slices"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func ReadAccess(filename string) (string, string, error) {
	ajson, err := os.Open("access.json")
	if err != nil {
		return "", "", err
	}
	defer ajson.Close()

	data, err := io.ReadAll(ajson)
	if err != nil {
		return "", "", err
	}

	var result struct {
		Token string
		Mongo string
	}
	err = json.Unmarshal(data, &result)
	if err != nil {
		return "", "", err
	}
	return result.Token, result.Mongo, nil
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

func KubeDanglings(active []int) ([]int, error) {
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
		return nil, err
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	namespaces, err := clientset.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var reviews []int
	for _, ns := range namespaces.Items {
		if strings.HasPrefix(ns.Name, "mirera-2-42-review-") {
			review, err := strconv.Atoi(strings.Split(ns.Name, "-")[4])
			if err != nil {
				return nil, err
			}
			if !slices.Contains(active, review) {
				reviews = append(reviews, review)
			}
		}
	}
	return reviews, nil
}

func MongoDanglings(ctx context.Context, uri string, active []int) ([]int, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, client.Disconnect(ctx))
	}()

	result, err := client.ListDatabaseNames(ctx, bson.D{{Key: "name", Value: bson.D{{Key: "$regex", Value: "^mirera-review"}}}})
	if err != nil {
		return nil, err
	}

	var dbs []int
	for _, db := range result {
		review, err := strconv.Atoi(strings.Split(db, "-")[2])
		if err != nil {
			return nil, err
		}
		if !slices.Contains(active, review) {
			dbs = append(dbs, review)
		}
	}
	return dbs, nil
}

func main() {
	ctx := context.TODO()
	token, mongo, err := ReadAccess("access.json")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(token, mongo)

	openMR, err := GetOpen(token)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(openMR)

	k8s_danglings, err := KubeDanglings(openMR)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(k8s_danglings)

	mongo_danglings, err := MongoDanglings(ctx, mongo, openMR)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(mongo_danglings)
}
