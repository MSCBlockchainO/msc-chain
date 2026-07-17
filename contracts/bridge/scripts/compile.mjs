import fs from "node:fs";
import path from "node:path";
import { artifact, compileContracts, projectRoot } from "./compile-lib.mjs";

const contracts = compileContracts();
const artifacts = [
  artifact(contracts, "contracts/MSCBridgeVault.sol", "MSCBridgeVault"),
  artifact(contracts, "contracts/MSCBridgeTronVault.sol", "MSCBridgeTronVault"),
  artifact(contracts, "contracts/test/MockUSDT.sol", "MockUSDT"),
];
const outputDirectory = path.join(projectRoot, "artifacts");
fs.mkdirSync(outputDirectory, { recursive: true });
for (const compiled of artifacts) {
  const outputPath = path.join(outputDirectory, `${compiled.contractName}.json`);
  fs.writeFileSync(outputPath, `${JSON.stringify(compiled, null, 2)}\n`);
  console.log(`Wrote ${path.relative(projectRoot, outputPath)}`);
}
