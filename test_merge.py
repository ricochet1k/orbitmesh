import re

with open('backend/internal/api/session_workflow_test.go', 'r') as f:
    content = f.read()

# Replace the conflict markers and keep the mock provider from main
new_content = re.sub(
    r'<<<<<<< HEAD.*?=======\s*(.*?)\s*>>>>>>> origin/main',
    r'\1',
    content,
    flags=re.DOTALL
)

with open('backend/internal/api/session_workflow_test.go', 'w') as f:
    f.write(new_content)
