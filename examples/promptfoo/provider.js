// Deterministic mock provider for the defrost smoke test. Returns
// hardcoded answers keyed off context.vars so CI can exercise the
// promptfoo adapter end-to-end without any LLM API calls.
module.exports = {
  id: () => 'defrost-smoke-mock',
  callApi: async (prompt, context) => {
    const country = context.vars.country;
    const capitals = {
      France: 'Paris',
      Germany: 'Berlin',
    };
    const answer = capitals[country];
    if (!answer) {
      return { output: `Unknown country: ${country}` };
    }
    return { output: answer };
  },
};
