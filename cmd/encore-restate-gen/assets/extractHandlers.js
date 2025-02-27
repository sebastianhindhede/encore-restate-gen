#!/usr/bin/env node
"use strict";

const { Project, SyntaxKind, Node } = require("ts-morph");
const fs = require("fs");
const path = require("path");

/**
 * Extracts handler definitions from a given TypeScript file.
 *
 * It looks for exported functions or variables (whose initializer is an arrow function,
 * function expression, or a call expression wrapping such a function) and then inspects
 * the first parameter's type annotation (assumed to be the context).
 *
 * The handler type is determined as follows:
 *   - If the type includes "WorkflowContext" or "WorkflowSharedContext": type is "workflow"
 *   - If the type includes "ObjectContext" or "ObjectSharedContext": type is "virtualObject"
 *   - Otherwise, if the type includes "Context": type is "service"
 *
 * The returned objects have:
 *   - exportName: the variable or function name (e.g. "greetHandler")
 *   - source: the relative path from the service directory to this file (as "./<basename>")
 *   - type: one of "service", "workflow", or "virtualObject"
 *
 * @param {string} filePath - Full path to the .ts file.
 * @param {string} targetDir - The service directory (where encore.service.ts resides).
 * @returns {Array<{exportName: string, source: string, type: string}>}
 */
function extractHandlersFromFile(filePath, targetDir) {
  try {
    const project = new Project({
      compilerOptions: { allowJs: true, target: 2 }
    });
    const sourceFile = project.addSourceFileAtPath(filePath);
    if (!sourceFile) {
      console.error(`Failed to load file: ${filePath}`);
      return [];
    }
    const results = [];
    // Get all exported declarations
    const exportedDeclarations = sourceFile.getExportedDeclarations();
    exportedDeclarations.forEach((declarations, exportName) => {
      for (const decl of declarations) {
        let func;
        if (Node.isFunctionDeclaration(decl)) {
          func = decl;
        } else if (Node.isVariableDeclaration(decl)) {
          const initializer = decl.getInitializer();
          if (initializer) {
            if (Node.isArrowFunction(initializer) || Node.isFunctionExpression(initializer)) {
              func = initializer;
            } else if (Node.isCallExpression(initializer)) {
              // If the initializer is a call, check its first argument.
              const args = initializer.getArguments();
              if (args.length > 0 && (Node.isArrowFunction(args[0]) || Node.isFunctionExpression(args[0]))) {
                func = args[0];
              }
            }
          }
        }
        if (!func) continue;
        const params = func.getParameters();
        if (params.length === 0) continue;
        const ctxParam = params[0];
        const typeNode = ctxParam.getTypeNode();
        if (!typeNode) continue;
        const typeText = typeNode.getText();
        let handlerType = null;
        if (typeText.includes("WorkflowContext") || typeText.includes("WorkflowSharedContext")) {
          handlerType = "workflow";
        } else if (typeText.includes("ObjectContext") || typeText.includes("ObjectSharedContext")) {
          handlerType = "virtualObject";
        } else if (typeText.includes("Context")) {
          handlerType = "service";
        }
        if (!handlerType) continue;
        const baseName = path.basename(filePath, ".ts");
        const relativeSource = "./" + baseName;
        results.push({ exportName, source: relativeSource, type: handlerType });
      }
    });
    return results;
  } catch (err) {
    console.error(`Error processing file ${filePath}: ${err}`);
    return [];
  }
}

/**
 * Extracts the service name from the specified encore.service.ts file.
 *
 * It looks for the default export and then searches for a new expression whose first argument
 * is a string literal representing the service name.
 *
 * @param {string} serviceFilePath
 * @returns {string|null}
 */
function extractServiceName(serviceFilePath) {
  if (!fs.existsSync(serviceFilePath)) {
    console.error(`Service file not found: ${serviceFilePath}`);
    return null;
  }
  try {
    const project = new Project({
      compilerOptions: { allowJs: true, target: 2 }
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
 *
 * Scans the target directory for .ts files (excluding encore.service.ts and generated files),
 * extracts handlers from each file, and outputs a JSON manifest.
 */
function main() {
  try {
    const targetDir = process.argv[2] || process.cwd();
    if (!fs.existsSync(targetDir) || !fs.statSync(targetDir).isDirectory()) {
      console.error(`Target directory does not exist or is not a directory: ${targetDir}`);
      process.exit(1);
    }
    const serviceFilePath = path.join(targetDir, "encore.service.ts");
    const serviceName = extractServiceName(serviceFilePath);
    if (!serviceName) {
      console.error("Error: Service name extraction failed.");
      process.exit(1);
    }
    const manifest = { serviceName, handlers: [] };
    const files = fs.readdirSync(targetDir);
    for (const file of files) {
      if (
        file.endsWith(".ts") &&
        file !== "encore.service.ts" &&
        !file.startsWith("restate.")
      ) {
        const filePath = path.join(targetDir, file);
        if (fs.statSync(filePath).isFile()) {
          const handlers = extractHandlersFromFile(filePath, targetDir);
          manifest.handlers.push(...handlers);
        }
      }
    }
    process.stdout.write(JSON.stringify(manifest, null, 2));
  } catch (err) {
    console.error("Unexpected error occurred: " + err);
    process.exit(1);
  }
}

main();
