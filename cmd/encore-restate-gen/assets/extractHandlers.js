#!/usr/bin/env node
"use strict";

// Production-ready embedded Node.js script using ts-morph
// This script extracts:
//   1. The service name from an adjacent `encore.service.ts` file by looking for a default export
//      that instantiates a Service (e.g. new Service("greeter")).
//   2. All calls to Restate.addHandler in other .ts files in the directory, extracting the handler name
//      and its function definition.
// It then outputs a JSON manifest on stdout.

const { Project, SyntaxKind } = require("ts-morph");
const fs = require("fs");
const path = require("path");

/**
 * Extracts handler definitions from a given TypeScript file.
 * Looks for call expressions of the form:
 *    Restate.addHandler('handlerName', async (ctx, ...) => { ... })
 *
 * @param {string} filePath - The full path to the .ts file.
 * @returns {Array<{name: string, body: string}>} Array of handler entries.
 */
function extractHandlersFromFile(filePath) {
  try {
    // Create a new ts-morph Project instance.
    const project = new Project({
      compilerOptions: {
        allowJs: true,
        target: 2, // ES6
      },
      // Optionally, you can specify tsconfig file path if available.
    });
    const sourceFile = project.addSourceFileAtPath(filePath);
    if (!sourceFile) {
      console.error(`Failed to load file: ${filePath}`);
      return [];
    }
    const results = [];
    const callExpressions = sourceFile.getDescendantsOfKind(SyntaxKind.CallExpression);
    for (const call of callExpressions) {
      try {
        // Identify calls to Restate.addHandler
        if (call.getExpression().getText() === "Restate.addHandler") {
          const args = call.getArguments();
          if (args.length >= 2) {
            // Extract the handler name and strip surrounding quotes.
            let handlerName = args[0].getText().trim();
            if (
              handlerName.startsWith("'") ||
              handlerName.startsWith("\"") ||
              handlerName.startsWith("`")
            ) {
              handlerName = handlerName.slice(1, -1);
            }
            // Get the full text of the handler function expression.
            const handlerBody = args[1].getText();
            results.push({ name: handlerName, body: handlerBody });
          } else {
            console.warn(
              `Warning: Restate.addHandler call in ${filePath} does not have at least 2 arguments.`
            );
          }
        }
      } catch (innerErr) {
        console.error(`Error processing a call in file ${filePath}: ${innerErr}`);
      }
    }
    return results;
  } catch (err) {
    console.error(`Error processing file ${filePath}: ${err}`);
    return [];
  }
}

/**
 * Extracts the service name from the specified encore.service.ts file.
 * Searches for the default export that creates a new Service instance.
 *
 * @param {string} serviceFilePath - The full path to encore.service.ts.
 * @returns {string|null} The extracted service name, or null if not found.
 */
function extractServiceName(serviceFilePath) {
  if (!fs.existsSync(serviceFilePath)) {
    console.error(`Service file not found: ${serviceFilePath}`);
    return null;
  }
  try {
    const project = new Project({
      compilerOptions: {
        allowJs: true,
        target: 2,
      },
    });
    const sourceFile = project.addSourceFileAtPath(serviceFilePath);
    if (!sourceFile) {
      console.error(`Failed to load service file: ${serviceFilePath}`);
      return null;
    }
    const defaultExportSymbol = sourceFile.getDefaultExportSymbol();
    if (defaultExportSymbol) {
      const declarations = defaultExportSymbol.getDeclarations();
      for (const decl of declarations) {
        const newExpr = decl.getFirstDescendantByKind(SyntaxKind.NewExpression);
        if (newExpr) {
          const args = newExpr.getArguments();
          if (args.length > 0) {
            let serviceName = args[0].getText().trim();
            if (
              serviceName.startsWith("'") ||
              serviceName.startsWith("\"") ||
              serviceName.startsWith("`")
            ) {
              serviceName = serviceName.slice(1, -1);
            }
            return serviceName;
          }
        }
      }
    }
    console.error(`Service name not found in ${serviceFilePath}`);
    return null;
  } catch (err) {
    console.error(`Error processing service file ${serviceFilePath}: ${err}`);
    return null;
  }
}

/**
 * Main entry point.
 * Determines the target directory (from command line or cwd),
 * extracts the service name and handler definitions, and outputs a JSON manifest.
 */
function main() {
  try {
    // Determine the target directory from the command-line arguments or default to the current working directory.
    const targetDir = process.argv[2] || process.cwd();
    if (!fs.existsSync(targetDir) || !fs.statSync(targetDir).isDirectory()) {
      console.error(`Target directory does not exist or is not a directory: ${targetDir}`);
      process.exit(1);
    }

    // Define the expected service file path.
    const serviceFilePath = path.join(targetDir, "encore.service.ts");
    const serviceName = extractServiceName(serviceFilePath);
    if (!serviceName) {
      console.error("Error: Service name extraction failed.");
      process.exit(1);
    }

    const manifest = {
      serviceName: serviceName,
      handlers: []
    };

    // Read and process each .ts file (except for encore.service.ts and any generated files).
    const files = fs.readdirSync(targetDir);
    for (const file of files) {
      if (
        file.endsWith(".ts") &&
        file !== "encore.service.ts" &&
        !file.startsWith("restate.")
      ) {
        const filePath = path.join(targetDir, file);
        if (fs.statSync(filePath).isFile()) {
          const handlers = extractHandlersFromFile(filePath);
          manifest.handlers.push(...handlers);
        }
      }
    }

    // Always output the manifest.
    process.stdout.write(JSON.stringify(manifest, null, 2));

  } catch (err) {
    console.error("Unexpected error occurred: " + err);
    process.exit(1);
  }
}

main();
