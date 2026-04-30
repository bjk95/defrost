// Deterministic mock provider for the defrost dogfood. Returns whatever the
// test case's `vars.answer` says — each test case in the YAML defines both
// its own input (`vars.question`) and the mock output (`vars.answer`), so
// assertions exercise the full variety of promptfoo assertion types
// without any LLM API calls.
//
// Modern promptfoo invokes file-based providers as `new ImportedClass(options)`
// — see promptfoo's loadApiProvider — so the export must be a class.
class DefrostMockProvider {
  constructor(options) {
    this.options = options || {};
  }

  id() {
    return this.options.id || 'defrost-smoke-mock';
  }

  async callApi(prompt, context) {
    return { output: context.vars.answer };
  }
}

module.exports = DefrostMockProvider;
