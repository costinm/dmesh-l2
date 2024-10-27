#!/bin/bash

# Run dmesh in a linux environment (pi, mips, etc)

DM_HOME=${DM_HOME:-$HOME/dmesh}
LOG_DIR=${LOG_DIR:-$DM_HOME/logs}

# Goland local:
# dlv --listen=localhost:9991 --headless=true --api-version=2 exec /ws/dmesh/bin/dmesh -
# dlv --listen=localhost:9991 --headless=true --api-version=2 attach 2088
# dlv --listen=localhost:9991 --headless=true --api-version=2 attach $(pgrep dmesh)

ulimit -Sn 1040000

function run() {
  mkdir -p logs
  mkdir -p bin
  stop

#  cp $HOME/www/arm/dmesh $DM_HOME/dmesh
#  (cd $DM_HOME; ./dmesh) > $LOG_DIR/dmesh.logs 2>&1  &
#  echo $! > $LOG_DIR/dmesh.pid


    while true; do

      cp ${HOME}/www/dmesh bin/dmesh
      DEBUG_DNS=1 VPNROOT=1 ./bin/dmesh 2>&1 | tee -a ${HOME}/dmesh.log

    done
}

function runfg() {
  stop
  (cd $DM_HOME; ./dmesh)
}

function stop() {
  if [[ -f $LOG_DIR/dmesh.pid ]] ; then
      kill -9 $(cat $LOG_DIR/dmesh.pid) || echo "No previous version"
  fi
  pkill dmesh || true
}

case $1 in
"run")
    runfg
    ;;
"start")
  run
    ;;
"stop")
  stop
    ;;
*)
  run
    ;;
esac

