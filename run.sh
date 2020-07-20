set -ex

go install .
rm -fr /tmp/bench
mkdir -p /tmp/bench
gsutil -m cp -r 'gs://istio-prow/benchmarks/*.txt' /tmp/bench
(cd $GOPATH/src/istio.io/istio; git log --format="format:%H,%cD" --date-order > /tmp/bench/commits)
benchstat-graph --commit-dates=/tmp/bench/commits /tmp/bench/*.txt
