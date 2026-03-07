export interface CodeflowQueryRequest {
  query: string;
}

export interface CodeflowQueryResult {
  columns: string[];
  rows: any[];
  plan?: any;
}
