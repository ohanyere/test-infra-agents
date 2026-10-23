const { appendFileSync } = require("node:fs");

function record(phase) {
  const path = process.env.OPERATION_PHASE_FILE;
  if (path) appendFileSync(path, `${new Date().toISOString()} ${phase}\n`);
}

module.exports = class OperationPhaseReporter {
  onBegin(_config, suite) {
    record(`runner ready: ${suite.allTests().length} tests`);
  }

  onTestBegin(test) {
    record(`test started: ${test.title}`);
  }

  onTestEnd(test, result) {
    record(`test ended: ${test.title} (${result.status})`);
  }

  onError(error) {
    record(`runner error: ${error.name || "unknown"}`);
  }
}
