import fs from "node:fs";
import path from "node:path";
import solc from "solc";

const root = path.resolve(import.meta.dirname, "..");

function readSource(relativePath) {
  return fs.readFileSync(path.join(root, relativePath), "utf8");
}

function resolveImport(importPath) {
  const candidates = [
    path.join(root, importPath),
    path.join(root, "node_modules", importPath),
  ];
  for (const candidate of candidates) {
    if (fs.existsSync(candidate)) {
      return { contents: fs.readFileSync(candidate, "utf8") };
    }
  }
  return { error: `Import not found: ${importPath}` };
}

export function compileContracts() {
  const input = {
    language: "Solidity",
    sources: {
      "contracts/MSCBridgeVault.sol": {
        content: readSource("contracts/MSCBridgeVault.sol"),
      },
      "contracts/MSCBridgeTronVault.sol": {
        content: readSource("contracts/MSCBridgeTronVault.sol"),
      },
      "contracts/test/MockUSDT.sol": {
        content: readSource("contracts/test/MockUSDT.sol"),
      },
    },
    settings: {
      optimizer: { enabled: true, runs: 1_000 },
      evmVersion: "cancun",
      metadata: { bytecodeHash: "none" },
      outputSelection: {
        "*": {
          "*": [
            "abi",
            "evm.bytecode.object",
            "evm.deployedBytecode.object",
            "metadata",
          ],
        },
      },
    },
  };

  const output = JSON.parse(solc.compile(JSON.stringify(input), { import: resolveImport }));
  const errors = (output.errors ?? []).filter((item) => item.severity === "error");
  if (errors.length > 0) {
    throw new Error(errors.map((item) => item.formattedMessage).join("\n"));
  }
  return output.contracts;
}

export function artifact(contracts, source, name) {
  const contract = contracts[source]?.[name];
  if (!contract) throw new Error(`Missing compiled contract ${source}:${name}`);
  return {
    contractName: name,
    sourceName: source,
    abi: contract.abi,
    bytecode: `0x${contract.evm.bytecode.object}`,
    deployedBytecode: `0x${contract.evm.deployedBytecode.object}`,
    metadata: JSON.parse(contract.metadata),
  };
}

export const projectRoot = root;
