@echo off
cd /d "c:\Users\Mohammad Talha\OneDrive\Desktop\msc-chain"
set MSC_VALIDATOR_PASSWORD_MODE=env_only
set MSC_VALIDATOR_PASSWORD=mfd@12g1
go run . --config config.toml --mode=full --id=A --port=7001 --datadir=data/A --rpcaddr 127.0.0.1:26657 1>> "c:\Users\Mohammad Talha\OneDrive\Desktop\msc-chain\validation_logs\fresh_f_join_20260307\A.out.log" 2>> "c:\Users\Mohammad Talha\OneDrive\Desktop\msc-chain\validation_logs\fresh_f_join_20260307\A.err.log"
