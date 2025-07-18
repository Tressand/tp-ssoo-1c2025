IP="192.168.0.96"
PORT="8082"
URL="$IP:$PORT"

curl -X POST "$URL/process?pid=1&size=64"
curl -X POST "$URL/process?pid=2&size=64"
curl -X POST "$URL/process?pid=3&size=0"
curl -X POST "$URL/process?pid=4&size=0"


curl "$URL/suspend?pid=3"
curl "$URL/suspend?pid=1"
curl "$URL/suspend?pid=4"
curl "$URL/suspend?pid=2"

curl "$URL/unsuspend?pid=2"
curl "$URL/unsuspend?pid=4"
curl "$URL/unsuspend?pid=1"
curl "$URL/unsuspend?pid=1"
curl "$URL/unsuspend?pid=3"