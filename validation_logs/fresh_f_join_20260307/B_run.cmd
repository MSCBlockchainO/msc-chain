@echo off
cd /d "c:\Users\Mohammad Talha\OneDrive\Desktop\msc-chain"
set MSC_VALIDATOR_PASSWORD_MODE=env_only
set MSC_VALIDATOR_PASSWORD=mfd@12g2
go run . --config config.toml --mode=full --id=B --port=7002 --datadir=data/B --rpcaddr 127.0.0.1:26658 1>> "c:\Users\Mohammad Talha\OneDrive\Desktop\msc-chain\validation_logs\fresh_f_join_20260307\B.out.log" 2>> "c:\Users\Mohammad Talha\OneDrive\Desktop\msc-chain\validation_logs\fresh_f_join_20260307\B.err.log"
