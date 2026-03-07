import { performance } from 'perf_hooks';

// Mock types
function generateLargeTree(depth, breadth) {
  let idCounter = 0;
  function createNode(d) {
    const node = {
      id: String(idCounter++),
      title: 'Task',
      role: 'Role' + (Math.random() * 10 | 0),
      status: 'pending',
      updated_at: '2023-01-01',
      children: []
    };
    if (d > 0) {
      for (let i = 0; i < breadth; i++) {
        node.children.push(createNode(d - 1));
      }
    }
    return node;
  }

  const roots = [];
  for (let i = 0; i < breadth; i++) {
    roots.push(createNode(depth));
  }
  return roots;
}

const treeData = generateLargeTree(6, 6); // ~ 335k nodes

function recursiveCollect1(treeData) {
  const unique = new Set();
  const collect = (nodes) => {
    nodes.forEach((node) => {
      unique.add(node.role);
      collect(node.children ?? []);
    });
  }
  collect(treeData);
  return Array.from(unique).sort();
}

function iterativeWithStackSpread(treeData) {
  const unique = new Set();
  const stack = [...treeData];

  while (stack.length > 0) {
    const node = stack.pop();
    unique.add(node.role);
    if (node.children && node.children.length > 0) {
      for (let i = 0; i < node.children.length; i++) {
        stack.push(node.children[i]);
      }
    }
  }

  return Array.from(unique).sort();
}

// Warmup
for (let i=0; i<10; i++) {
  recursiveCollect1(treeData);
  iterativeWithStackSpread(treeData);
}

const RUNS = 50;

let start1 = performance.now();
for (let i=0; i<RUNS; i++) recursiveCollect1(treeData);
let end1 = performance.now();

let start2 = performance.now();
for (let i=0; i<RUNS; i++) iterativeWithStackSpread(treeData);
let end2 = performance.now();

console.log(`Original Recursive (forEach): ${((end1 - start1)/RUNS).toFixed(2)}ms per run`);
console.log(`Iterative (Stack spread): ${((end2 - start2)/RUNS).toFixed(2)}ms per run`);
