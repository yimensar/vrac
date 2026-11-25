#!/bin/bash

# Raycast script command: writes one line to today's Vrac journal.
#
# Setup: put this file in a Raycast script directory, then store the
# instance URL and a personal token (Settings → Connections) in the
# keychain:
#   security add-generic-password -s vrac -a url -w https://vrac.example.com
#   security add-generic-password -s vrac -a token -w vrac_…
#
# @raycast.schemaVersion 1
# @raycast.title Capture in Vrac
# @raycast.mode silent
# @raycast.packageName Vrac
# @raycast.icon 📝
# @raycast.argument1 { "type": "text", "placeholder": "Line, #tags and [[names]] allowed" }

set -euo pipefail

url=$(security find-generic-password -s vrac -a url -w)
token=$(security find-generic-password -s vrac -a token -w)

# JSON string escaping for one line of text.
text=$1
text=${text//\\/\\\\}
text=${text//\"/\\\"}
text=${text//$'\t'/\\t}
text=${text//$'\n'/\\n}

response=$(curl --silent --show-error --fail-with-body --max-time 10 \
  -H "Authorization: Bearer $token" -H "Content-Type: application/json" \
  -d "{\"request_id\":\"raycast-$(uuidgen)\",\"text\":\"$text\"}" \
  "$url/api/v1/capture") || { echo "Not saved: $response"; exit 1; }

if [[ $response == *'"unresolved"'* ]]; then
  echo "Saved; unknown names kept as plain words"
else
  echo "Saved in Vrac"
fi
