@echo off
cd /d "C:\Users\Mohammad Talha\OneDrive\Desktop\msc-chain"
set MSC_VALIDATOR_PASSWORD=mfd@12g1
go run . --config config.toml --mode=full --id=A --port=7001 --datadir=data/A --rpcaddr 127.0.0.1:26657 1>runtime-logs\bootstrap_A.log 2>runtime-logs\bootstrap_A.err.log
