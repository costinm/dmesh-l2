#!/bin/bash

# Build and launch in debug mode

function install() {
    deploy 10.1.10.1
}

function deploy() {
    local H=$1

    scp $GOPATH/bin/dmesh ${H}:
    ssh $H /bin/bash -x <<EOF
    sudo setcap cap_net_admin+ep ./dmesh
    if [[ -f dmesh.pid ]] ; then
      kill -9 $(cat dmesh.pid) || echo "No previous version"
    fi
    ./dmesh > dmesh.logs 2>&1 &
    echo $! > dmesh.pid
EOF
}

function run() {
    pushd $GOPATH

    mkdir -p logs
    if [[ -f logs/dmesh.pid ]]; then
        kill -9 $(cat logs/dmesh.pid) || echo "No previous version"
        kill -9 $(cat logs/dmesh_dlv.pid) || echo "No previous version"
    fi
    $GOPATH/bin/dmesh >logs/dmesh.logs 2>&1 &
    echo $! >logs/dmesh.pid

    sudo $GOPATH/bin/dlv --listen=:2345 --headless=true --api-version=2 --init="continue" attach $(cat logs/dmesh.pid) &
    echo $! >logs/dmesh_dlv.pid

    echo "Started"
    popd
}

function status() {
    curl http://localhost:5220/debug/vars
    echo -e "\nLocal:\n"
    curl http://localhost:5220/c4
    echo -e "\nAll:\n"
    curl http://localhost:5220/c

}

function runfg() {
    mkdir -p $GOPATH/logs
    if [[ -f $GOPATH/logs/dmesh.pid ]]; then
        kill -9 $(cat $GOPATH/logs/dmesh.pid) || echo "No previous version"
        kill -9 $(cat $GOPATH/logs/dmesh_dlv.pid) || echo "No previous version"
    fi
    (
        cd $GOPATH
        go install -gcflags='-N -l' github.com/costinm/dmesh
    )

    sudo setcap cap_net_admin+ep $GOPATH/bin/dmesh
    (
        cd $GOPATH
        $GOPATH/bin/dmesh | tee $GOPATH/logs/dmesh.logs 2>&1
    )
}

function runDocker() {
    #  cat /etc/docker/daemon.json
    #{
    #"ipv6": true,
    #"fixed-cidr-v6": "2001:db8:1::/64"
    #}

    docker run -it --rm -v $GOPATH:/go golang bash -c "go run src/github.com/costinm/dmesh/dmesh.go"
}

function captureDNSRedir() {
    # Consul: 8600
    iptables -t nat -A OUTPUT -d localhost -p udp -m udp --dport 53 -j REDIRECT --to-ports 6353
    iptables -t nat -A OUTPUT -d localhost -p tcp -m tcp --dport 53 -j REDIRECT --to-ports 6353
}

function captureDNSStop() {
    iptables -t nat -D OUTPUT -d localhost -p udp -m udp --dport 53 -j REDIRECT --to-ports 6353
    iptables -t nat -D OUTPUT -d localhost -p tcp -m tcp --dport 53 -j REDIRECT --to-ports 6353
}

function dbgPriv() {
    go get -v github.com/costinm/dmesh
    GOARCH=arm go install -gcflags='-N -l' github.com/costinm/dmesh
    go install -gcflags='-N -l' github.com/costinm/dmesh

    cd $
    GOPATH

    sudo setcap cap_net_admin+ep bin/dmesh

    scp bin/dmesh s2:

    if [[ -f dmesh.pid ]]; then
        kill -9 $(cat dmesh.pid) || echo "No previous version"
    fi

    while getopts w:t: arg; do
        case "${arg}" in
        w) PROJECT_ID="${OPTARG}" ;;
        t) TAG_NAME="${OPTARG}" ;;
            #    *) usage;;
        esac
    done

    if [[ ${1:-} == "wait" ]]; then
        dlv --listen=:2345 --headless=true --api-version=2 exec bin/dmesh
    elif [[ ${1:-} == "fg" ]]; then
        bin/dmesh
    else
        $GOPATH/bin/dmesh &
        echo $! >dmesh.pid

        dlv --listen=:2345 --headless=true --api-version=2 --init="continue" attach $(cat $GOPATH/dmesh.pid)
        echo "Started"

    fi

}

function runDockerTest() {
    #  cat /etc/docker/daemon.json
    #{
    #"ipv6": true,
    #"fixed-cidr-v6": "2001:db8:1::/64"
    #}

    docker run -it --rm -v $GOPATH:/go golang bash -c "go test github.com/costinm/dmesh"
}

case $1 in
"wait")
    dlv --listen=:2345 --headless=true --api-version=2 exec bin/dmesh
    ;;
"env")
    echo "build install run"
    ;;
"run")
    runfg
    ;;
"build")
    build
    ;;
"fg")
    bin/dmesh
    ;;
"status")
    status
    ;;
*) ;;
esac
