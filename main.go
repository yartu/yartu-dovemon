package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/remotecommand"
)

func execCmd(client kubernetes.Interface, config *restclient.Config, podName string, command string) error {
	cmd := []string{
		"sh",
		"-c",
		command,
	}
	req := client.CoreV1().RESTClient().Post().Resource("pods").Name(podName).Namespace("default").SubResource("exec")
	option := &v1.PodExecOptions{
		Command: cmd,
		Stdin:   false,
		Stdout:  true,
		Stderr:  true,
		TTY:     true,
	}

	req.VersionedParams(
		option,
		scheme.ParameterCodec,
	)
	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return err
	}

	buf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}

	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: buf,
		Stderr: errBuf,
	})
	if err != nil {
		return err
	}

	return nil
}

func refresh(event string, oldObj interface{}, newObj interface{}, clientset *kubernetes.Clientset, config *restclient.Config, dovecotPreName string, directorPreName string) {

	var newPod *v1.Pod = nil
	var oldPod *v1.Pod = nil
	var podName string

	if newObj != nil {
		newPod = newObj.(*v1.Pod)
		podName = newPod.ObjectMeta.Name
	}

	if oldObj != nil {
		oldPod = oldObj.(*v1.Pod)
		podName = oldPod.ObjectMeta.Name
	}

	if strings.Contains(podName, dovecotPreName) || strings.Contains(podName, directorPreName) {
		if event == "delete" {
			if oldPod.Status.Phase != "Pending" {
				fmt.Printf("Pod deleted: %s \n", oldPod.ObjectMeta.Name)
			} else {
				return
			}
		} else if event == "update" {
			if len(oldPod.Status.ContainerStatuses) > 0 && len(newPod.Status.ContainerStatuses) > 0 {
				oldReady := oldPod.Status.ContainerStatuses[0].Ready
				newReady := newPod.Status.ContainerStatuses[0].Ready
				if (oldReady || newReady) && !(oldReady && newReady) {
					fmt.Printf("Pod updated: %s  ===  %v -> %v \n", newPod.ObjectMeta.Name, oldPod.Status.ContainerStatuses[0].Ready, newPod.Status.ContainerStatuses[0].Ready)
				} else {
					return
				}
			} else {
				return
			}
		} else {
			return
		}

		options := metav1.ListOptions{
			LabelSelector: "app=" + directorPreName,
		}
		podList, _ := clientset.CoreV1().Pods("default").List(context.TODO(), options)

		var command string

		if strings.Contains(podName, dovecotPreName) {
			if event == "delete" {
				oldIP := oldPod.Status.PodIP
				command = fmt.Sprintf("doveadm director update %s 0 && doveadm director flush %s && doveadm director remove %s", oldIP, oldIP, oldIP)
			} else if event == "update" {
				newIP := newPod.Status.PodIP
				command = fmt.Sprintf("doveadm director add %s", newIP)
			}
		} else if strings.Contains(podName, directorPreName) {
			if event == "delete" {
				return
			}
			command = "doveadm reload"
		}

		for _, podInfo := range (*podList).Items {
			if podInfo.Status.Phase == "Running" {
				fmt.Printf("Pod to exec command: %s\nCommand: %s\n", podInfo.Name, command)

				err := execCmd(clientset, config, podInfo.Name, command)
				if err != nil {
					log.Println(err)
				}
			}
		}

		fmt.Printf("\n\n\n")
	}
}

func main() {
	dovecotPreName := flag.String("dovecot-prename", "yartu-dovecot", "")
	directorPreName := flag.String("director-prename", "yartu-director", "")
	flag.Parse()

	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	watchlist := cache.NewListWatchFromClient(
		clientset.CoreV1().RESTClient(),
		string(v1.ResourcePods),
		v1.NamespaceAll,
		fields.Everything(),
	)
	_, controller := cache.NewInformer(
		watchlist,
		&v1.Pod{},
		0,
		cache.ResourceEventHandlerFuncs{
			DeleteFunc: func(obj interface{}) {
				refresh("delete", obj, nil, clientset, config, *dovecotPreName, *directorPreName)
			},
			UpdateFunc: func(oldObj interface{}, newObj interface{}) {
				refresh("update", oldObj, newObj, clientset, config, *dovecotPreName, *directorPreName)
			},
		},
	)

	stop := make(chan struct{})
	defer close(stop)
	go controller.Run(stop)
	for {
		time.Sleep(time.Second)
	}

}
