import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import {
  mergeTronReleaseArtifacts,
  prepareTronRelease,
  signTronReleaseArtifact,
  validateTronReleaseArtifact,
} from "./tron-release-lib.mjs";

const MAX_FILE_BYTES = 2 * 1024 * 1024;

function usage() {
  console.error("Usage:");
  console.error("  npm run release:tron -- prepare --deployment <record.json> --unlock <gateway.json> --valid-until <unix> --output <artifact.json>");
  console.error("  npm run release:tron -- sign --input <artifact.json> --key-env <ENV_NAME> --output <signed.json>");
  console.error("  npm run release:tron -- merge --input <signed-a.json> --input <signed-b.json> --output <merged.json>");
  console.error("  npm run release:tron -- verify --input <artifact.json>");
}

function parseArgs(argv) {
  const command = argv.shift();
  if (!new Set(["prepare", "sign", "merge", "verify"]).has(command)) {
    throw new Error("invalid command");
  }
  const values = {};
  while (argv.length > 0) {
    const flag = argv.shift();
    const value = argv.shift();
    if (!/^--[a-z-]+$/.test(flag ?? "") || !value) throw new Error("invalid arguments");
    const key = flag.slice(2);
    if (key === "input") {
      values.input ??= [];
      values.input.push(value);
    } else {
      if (values[key]) throw new Error(`duplicate ${flag}`);
      values[key] = value;
    }
  }
  return { command, values };
}

function readStrictJSON(filePath) {
  const absolute = path.resolve(filePath);
  const stat = fs.lstatSync(absolute);
  if (!stat.isFile() || stat.isSymbolicLink() || stat.size <= 0 || stat.size > MAX_FILE_BYTES) {
    throw new Error("input must be a regular JSON file between 1 byte and 2 MB");
  }
  return JSON.parse(fs.readFileSync(absolute, "utf8"));
}

function writeImmutable(filePath, value) {
  const absolute = path.resolve(filePath);
  fs.mkdirSync(path.dirname(absolute), { recursive: true });
  fs.writeFileSync(absolute, `${JSON.stringify(value, null, 2)}\n`, {
    flag: "wx",
    mode: 0o600,
  });
  return absolute;
}

async function main() {
  const { command, values } = parseArgs(process.argv.slice(2));
  if (command === "prepare") {
    if (!values.deployment || !values.unlock || !values["valid-until"] || !values.output) {
      throw new Error("prepare arguments are incomplete");
    }
    const artifact = prepareTronRelease({
      deployment: readStrictJSON(values.deployment),
      unlockInstruction: readStrictJSON(values.unlock),
      validUntil: values["valid-until"],
      nowUnix: Math.floor(Date.now() / 1000),
    });
    console.log(`Prepared ${artifact.gateway.withdrawalID} -> ${writeImmutable(values.output, artifact)}`);
    return;
  }
  if (command === "sign") {
    if (values.input?.length !== 1 || !values["key-env"] || !values.output ||
        !/^[A-Z][A-Z0-9_]{2,127}$/.test(values["key-env"])) {
      throw new Error("sign arguments are incomplete");
    }
    const privateKey = process.env[values["key-env"]];
    if (!/^(?:0x)?[0-9a-fA-F]{64}$/.test(privateKey ?? "")) {
      throw new Error(`${values["key-env"]} must contain one 32-byte hex TRON committee key`);
    }
    delete process.env[values["key-env"]];
    const artifact = await signTronReleaseArtifact(readStrictJSON(values.input[0]), privateKey);
    const signer = artifact.signatures.at(-1)?.signer ?? "unknown";
    console.log(`Signed TRON release (${signer}) -> ${writeImmutable(values.output, artifact)}`);
    return;
  }
  if (command === "merge") {
    if ((values.input?.length ?? 0) < 2 || !values.output) {
      throw new Error("merge requires at least two inputs and an output");
    }
    const artifact = mergeTronReleaseArtifacts(values.input.map(readStrictJSON));
    console.log(`Merged ${artifact.signatures.length}/${artifact.committee.threshold} signatures -> ${writeImmutable(values.output, artifact)}`);
    return;
  }
  if (values.input?.length !== 1) throw new Error("verify requires exactly one input");
  const result = validateTronReleaseArtifact(readStrictJSON(values.input[0]), true);
  console.log(
    `Valid withdrawal=${result.artifact.gateway.withdrawalID} signers=${result.recoveredSigners.length} endpoint=${result.artifact.transaction.endpoint}`,
  );
}

main().catch((error) => {
  usage();
  console.error(error?.message || String(error));
  process.exitCode = 1;
});
