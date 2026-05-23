@echo off
cd /d "c:\Users\Mohammad Talha\OneDrive\Desktop\msc-chain"
set MSC_VALIDATOR_PASSWORD_MODE=env_only
set MSC_VALIDATOR_PASSWORD=mfd@12g3
go run . --config config.toml --mode=full --id=D --port=7004 --datadir=data/D --rpcaddr 127.0.0.1:26660 1>> "c:\Users\Mohammad Talha\OneDrive\Desktop\msc-chain\validation_logs\fresh_f_join_20260307\D.out.log" 2>> "c:\Users\Mohammad Talha\OneDrive\Desktop\msc-chain\validation_logs\fresh_f_join_20260307\D.err.log"
