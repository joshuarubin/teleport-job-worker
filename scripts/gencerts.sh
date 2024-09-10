#!/usr/bin/env bash

set -e

mkdir -p assets/tls
cd assets/tls

for type in ca server client0 client1; do
  if [ ! -f "${type}_key.pem" ]; then
    openssl ecparam -name secp256r1 -genkey -out "${type}_key.pem"
  fi
done

## CA, self-signed
if [ ! -f ca_cert.pem ]; then
  openssl req -x509 -nodes -days 3650 -key ca_key.pem -config ca_cert_config.txt -extensions req_ext -nameopt utf8 -utf8 -out ca_cert.pem
fi

for c in server client0 client1; do
  if [ ! -f "${c}_csr.pem" ]; then
    openssl req -new -nodes -key "${c}_key.pem" -config "${c}_csr_config.txt" -nameopt utf8 -utf8 -out "${c}_csr.pem"
  fi

  if [ ! -f "${c}_cert.pem" ]; then
    openssl x509 -req -in "${c}_csr.pem" -days 3650 -CA ca_cert.pem -CAkey ca_key.pem -extfile "${c}_cert_config.txt" -extensions req_ext -CAserial ca_serial.txt -nameopt utf8 -sha256 -out "${c}_cert.pem"
  fi
done
