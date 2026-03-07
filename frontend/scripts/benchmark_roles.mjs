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

const treeData = generateLargeTree(6, 6); // 6^7 nodes ~ 279k nodes
console.log(`Generated tree with ${countNodes(treeData)} nodes.`);

function countNodes(nodes) {
  let count = 0;
  const stack = [...nodes];
  while(stack.length > 0) {
    const n = stack.pop();
    count++;
    if(n.children) stack.push(...n.children);
  }
  return count;
}

function recursiveCollect(treeData) {
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

function iterativeCollect(treeData) {
  const unique = new Set();
  const stack = [];
  for (let i = 0; i < treeData.length; i++) {
    stack.push(treeData[i]);
  }

  while (stack.length > 0) {
    const node = stack.pop();
    unique.add(node.role);
    const children = node.children;
    if (children) {
      for (let i = 0; i < children.length; i++) {
        stack.push(children[i]);
      }
    }
  }
  return Array.from(unique).sort();
}

// Warmup
for (let i=0; i<10; i++) {
  recursiveCollect(treeData);
  iterativeCollect(treeData);
}

const RUNS = 50;

let start1 = performance.now();
for (let i=0; i<RUNS; i++) recursiveCollect(treeData);
let end1 = performance.now();

let start2 = performance.now();
for (let i=0; i<RUNS; i++) iterativeCollect(treeData);
let end2 = performance.now();

console.log(`Recursive: ${((end1 - start1)/RUNS).toFixed(2)}ms per run`);
console.log(`Iterative: ${((end2 - start2)/RUNS).toFixed(2)}ms per run`);
