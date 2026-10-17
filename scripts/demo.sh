#!/bin/sh
set -eu

base_url="${BASE_URL:-http://localhost:8080}"
cookie_jar="${TMPDIR:-/tmp}/media-sessiond-cookies.txt"

curl --fail-with-body --silent --show-error \
  --request POST "$base_url/signup" \
  --header 'Content-Type: application/json' \
  --cookie-jar "$cookie_jar" \
  --data '{"email":"maker@example.com","password":"correct horse battery staple","name":"Maker","captcha_token":"replace-with-browser-token","request_id":"signup-demo-001"}'

curl --fail-with-body --silent --show-error \
  --request POST "$base_url/assets" \
  --header 'Content-Type: application/json' \
  --cookie "$cookie_jar" \
  --data '{"id":"asset-demo-001","title":"Launch cut","source":"s3://media-inbox/launch.mov"}'

sleep 1
curl --fail-with-body --silent --show-error \
  --request GET "$base_url/creator/assets/asset-demo-001" \
  --cookie "$cookie_jar"
