// Stub — outerbase-cloud APIs removed. Only the minimal types needed by
// retained components are exported here.
export class OuterbaseAPIError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "OuterbaseAPIError";
  }
}
