#!/bin/sh
script_path=$(dirname "$0")
cd $script_path
go build --tags "fts5" "-ldflags=-s" -o $script_path/magneticod $script_path/cmd/magneticod/main.go;

$script_path/magneticod -v --database=beanstalk://yourBeansTalkIP:yourBeansTalkPort/magneticod_tube --indexer-max-neighbors=50000 --indexer-interval=2
