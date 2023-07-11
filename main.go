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
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/exp/slices"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Minio struct {
	Endpoint string `json:"endpoint"`
	Access   string `json:"access"`
	Secret   string `json:"secret"`
}

type Access struct {
	Gitlab  string `json:"gitlab"`
	Mongo   string `json:"mongo"`
	MinioAc Minio  `json:"minio"`
}

func ReadAccess(filename string) (Access, error) {
	ajson, err := os.Open("access.json")
	if err != nil {
		return Access{}, err
	}
	defer ajson.Close()

	data, err := io.ReadAll(ajson)
	if err != nil {
		return Access{}, err
	}

	var result Access
	err = json.Unmarshal(data, &result)
	if err != nil {
		return Access{}, err
	}
	return result, nil
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

func KubeDanglings(ctx context.Context, dang chan<- int, done chan<- bool, active []int) error {
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
	namespaces, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, ns := range namespaces.Items {
		if strings.HasPrefix(ns.Name, "mirera-2-42-review-") {
			review, err := strconv.Atoi(strings.Split(ns.Name, "-")[4])
			if err != nil {
				return err
			}
			if !slices.Contains(active, review) {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case dang <- review:
				}
			}
		}
	}
	done <- true
	return nil
}

func MongoDanglings(ctx context.Context, dang chan<- int, done chan<- bool, uri string, active []int) error {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, client.Disconnect(ctx))
	}()

	result, err := client.ListDatabaseNames(ctx, bson.D{{Key: "name", Value: bson.D{{Key: "$regex", Value: "^mirera-review"}}}})
	if err != nil {
		return err
	}

	for _, db := range result {
		review, err := strconv.Atoi(strings.Split(db, "-")[2])
		if err != nil {
			return err
		}
		if !slices.Contains(active, review) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case dang <- review:
			}
		}
	}
	done <- true
	return nil
}

func MinioDanglings(ctx context.Context, dang chan<- int, done chan<- bool, access Minio, active []int) error {
	useSSL := false
	client, err := minio.New(access.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(access.Access, access.Secret, ""),
		Secure: useSSL,
	})
	if err != nil {
		return err
	}

	buckets, err := client.ListBuckets(ctx)
	if err != nil {
		return err
	}
	for _, bucket := range buckets {
		if regexp.MustCompile(`^mirera-[0-9]+`).MatchString(bucket.Name) {
			review, err := strconv.Atoi(strings.Split(bucket.Name, "-")[1])
			if err != nil {
				return err
			}
			if !slices.Contains(active, review) {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case dang <- review:
				}
			}
		}
	}
	done <- true
	return nil
}

func main() {
	access, err := ReadAccess("access.json")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(access)

	openMR, err := GetOpen(access.Gitlab)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(openMR)

	grp, ctx := errgroup.WithContext(context.Background())
	done := make(chan bool)
	k8s_danglings := make(chan int)
	grp.Go(func() error { return KubeDanglings(ctx, k8s_danglings, done, openMR) })

	mongo_danglings := make(chan int)
	grp.Go(func() error { return MongoDanglings(ctx, mongo_danglings, done, access.Mongo, openMR) })

	minio_danglings := make(chan int)
	grp.Go(func() error { return MinioDanglings(ctx, minio_danglings, done, access.MinioAc, openMR) })

	type Dang struct {
		N   int
		Src string
	}
	danglings := make(chan Dang)
	grp.Go(func() error {
		defer close(danglings)
		var d int
		for n := 3; n > 0; {
			select {
			case d = <-k8s_danglings:
				danglings <- Dang{d, "k8s"}
			case d = <-mongo_danglings:
				danglings <- Dang{d, "mongo"}
			case d = <-minio_danglings:
				danglings <- Dang{d, "minio"}
			case <-done:
				n--
			}
		}
		return nil
	})

	grp.Go(func() error {
		for d := range danglings {
			fmt.Println(d)
		}
		return nil
	})
	if err := grp.Wait(); err != nil {
		log.Fatal(err)
	}
}
