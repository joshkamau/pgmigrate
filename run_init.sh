#! /bin/sh

#clean
rm -rvf ./data

#setup
mkdir ./data

#build
go build

#run
./pgmigrate init ./data


#delete binary
rm pgmigrate
