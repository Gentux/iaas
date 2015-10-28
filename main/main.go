package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/rpc"
	"net/rpc/jsonrpc"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/natefinch/pie"
	"gopkg.in/fsnotify.v1"
)

type plugin struct {
	name   string
	client *rpc.Client
}

var (
	plugins  = make(map[string]plugin)
	run_dir  = "plugins/running/"
	stag_dir = "plugins/staging/"
	inst_dir = "plugins/installed/"
)

func core(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("CORE PAGE"))

}

func get_plugins() []string {
	dir, err := os.Open("plugins/running")
	checkErr(err)
	defer dir.Close()
	fi, err := dir.Stat()
	checkErr(err)
	filenames := make([]string, 0)
	if fi.IsDir() {
		fis, err := dir.Readdir(-1) // -1 means return all the FileInfos
		checkErr(err)
		for _, fileinfo := range fis {
			if !fileinfo.IsDir() {
				filenames = append(filenames, fileinfo.Name())
			}
		}
	}
	log.Println("Files: ", filenames)
	return filenames
}

func checkErr(err error) {
	if err != nil {
		fmt.Println("Error :")
		fmt.Println(err)

	}
}

func launch_existing_plugins(running_plugins []string) []string {
	plugs := get_plugins()
	for _, plugin := range plugs {
		running_plugins = LoadPlugin(running_plugins, plugin)
		CopyFile(run_dir+plugin, inst_dir+plugin)

	}
	return running_plugins
}

func main() {

	//go handlz()
	go func() {
		http.HandleFunc("/", core)
		err := http.ListenAndServe(":9092", nil)
		if err != nil {
			log.Fatal("ListenAndServe: ", err)
		}
	}()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer w.Close()

	var running_plugins = make([]string, 0)
	running_plugins = launch_existing_plugins(running_plugins)
	watchPlugins(w, running_plugins)

	select {} // wait forever
}

func ClosePlugin(running_plugins []string, name string) []string {
	for i, val := range running_plugins {
		if val == name {
			closePlugin(run_dir + name)
			running_plugins = append(running_plugins[:i], running_plugins[i+1:]...)
			fmt.Println("deleted plugin from slice")
		}
	}
	return running_plugins
}

func LoadPlugin(running_plugins []string, name string) []string {

	loadPlugin(run_dir + name)
	fmt.Println(name)
	running_plugins = append(running_plugins, name)
	fmt.Println("added plugin to slice")

	return running_plugins
}

func IsRunning(running_plugins []string, name string) bool {
	check := false
	for _, val := range running_plugins {
		if val == name {
			fmt.Println("GOT ITTTTTT")
			check = true
		}
	}
	return check
}

func CopyFile(source string, dest string) (err error) {
	sf, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sf.Close()
	df, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer df.Close()
	_, err = io.Copy(df, sf)
	if err == nil {
		si, err := os.Stat(source)
		if err != nil {
			err = os.Chmod(dest, si.Mode())
		}
	}
	return
}

func DeletePlugin(path string) {
	oldpath := path
	if _, ok := plugins[path]; !ok {
		path = path[:8] + "staging" + path[15:]
		fmt.Println(path)

	}

	if path == oldpath {
		err := os.Remove(path)
		if err != nil {
			fmt.Println(err)
		}
	} else {
		err := os.Remove(oldpath)
		if err != nil {
			fmt.Println(err)
		}
	}
}

func CreateEvent(running_plugins []string, name string, fullpath string) []string {
	time.Sleep(1000 * time.Millisecond)
	if IsRunning(running_plugins, name) {
		running_plugins = ClosePlugin(running_plugins, name)
		err := loadPlugin(fullpath)
		if err != nil {
			fmt.Println("error loading plugin")
			fmt.Println(err)
		}
		if plugins[fullpath].Check() == true {
			running_plugins = append(running_plugins, name)
			fmt.Println("added previously existing plugin to slice")
			DeletePlugin(run_dir + name)
			os.Rename(stag_dir+name, run_dir+name)
			CopyFile(run_dir+name, inst_dir+name)
		} else {
			fmt.Println("New plugin encountered an error")
			err := loadPlugin(run_dir + name)
			if err != nil {
				fmt.Println("error loading plugin")
				fmt.Println(err)
			}
		}

	} else {
		os.Rename(stag_dir+name, run_dir+name)
		CopyFile(run_dir+name, inst_dir+name)
		running_plugins = LoadPlugin(running_plugins, name)

	}
	return running_plugins
}

func watchPlugins(w *fsnotify.Watcher, running_plugins []string) {
	go func() {

		for {
			select {
			case evt := <-w.Events:
				log.Println("fsnotify:", evt)
				switch evt.Op {
				case fsnotify.Create:
					if evt.Name[:16] == stag_dir {
						running_plugins = CreateEvent(running_plugins, evt.Name[16:], evt.Name)
					}
				case fsnotify.Remove:
					closePlugin(run_dir + evt.Name[18:])
					DeletePlugin(run_dir + evt.Name[18:])
					fmt.Println(running_plugins)
				}

			case err := <-w.Errors:
				log.Println("watcher crashed:", err)

			}

		}
	}()
	w.Add(stag_dir)
	w.Add(inst_dir)

}

func loadPlugin(path string) error {

	c, err := pie.StartProviderCodec(jsonrpc.NewClientCodec, os.Stderr, path)
	if err != nil {
		log.Printf("Error running plugin %s: %s", path, err)
		return err
	}

	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	p := plugin{
		name:   name,
		client: c,
	}
	p.Plug()

	plugins[path] = p

	return nil
}

func closePlugin(path string) {
	fmt.Println("CLEAN DELETION")
	fmt.Println(path)
	if _, ok := plugins[path]; !ok {
		path = path[:8] + "staging" + path[15:]
		fmt.Println(path)
		if _, ok := plugins[path]; !ok {
			fmt.Println("Plugin not found for deletion")
			return
		}
	}

	plugins[path].Unplug()

	delete(plugins, path)
}

func (p plugin) Plug() {
	var reply bool
	err := p.client.Call(p.name+".Plug", nil, &reply)
	if err != nil {
		log.Println("Error while calling Plug:", err)
	}
	log.Println(p.name + " plugged")
}

func (p plugin) Check() bool {
	reply := false
	err := p.client.Call(p.name+".Check", nil, &reply)
	if err != nil {
		log.Println("Error while calling Check:", err)
	}
	log.Println(p.name + " checked")
	return reply
}

func (p plugin) Unplug() {
	var reply bool
	err := p.client.Call(p.name+".Unplug", nil, &reply)
	if err != nil && err != io.ErrUnexpectedEOF {
		log.Println("Error while calling Unplug:", err)
	}
	p.client.Close()
	log.Println(p.name + " unplugged")
}