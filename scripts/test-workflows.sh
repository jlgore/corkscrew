#!/bin/bash
set -euo pipefail

# Test workflows locally with act
echo "🧪 Testing GitHub workflows locally with act"

# Function to run a workflow with act
run_workflow() {
    local workflow=$1
    local event=${2:-"push"}
    local inputs=${3:-""}
    
    echo ""
    echo "▶️  Testing workflow: $workflow (event: $event)"
    echo "=================================================="
    
    local act_cmd="act $event -W .github/workflows/$workflow"
    
    # Add inputs if provided
    if [[ -n "$inputs" ]]; then
        act_cmd="$act_cmd $inputs"
    fi
    
    # Add common flags and GitHub token from gh cli
    act_cmd="$act_cmd -s GITHUB_TOKEN=\"\$(gh auth token)\" --verbose --dryrun"
    
    echo "Command: $act_cmd"
    echo ""
    
    # Run with timeout to prevent hanging
    timeout 300s $act_cmd || {
        local exit_code=$?
        if [[ $exit_code -eq 124 ]]; then
            echo "⏰ Workflow test timed out after 5 minutes"
        else
            echo "❌ Workflow test failed with exit code: $exit_code"
        fi
        return $exit_code
    }
    
    echo "✅ Workflow test completed"
}

# Test the main build workflow
echo "Testing build-and-publish.yml..."
run_workflow "build-and-publish.yml" "push"

echo ""
echo "Testing build-and-publish.yml with workflow_dispatch..."
run_workflow "build-and-publish.yml" "workflow_dispatch"

echo ""
echo "Testing provider-test.yml..."
run_workflow "provider-test.yml" "workflow_dispatch" '--input provider=aws --input scenario=simple'

echo ""
echo "🎉 All workflow tests completed!"
echo ""
echo "📋 Summary:"
echo "- build-and-publish.yml: Tests CLI and plugin building"
echo "- provider-test.yml: Tests provider integration"
echo ""
echo "💡 To run a specific workflow without dry-run:"
echo "   act push -W .github/workflows/build-and-publish.yml -s GITHUB_TOKEN=\"\$(gh auth token)\""
echo ""
echo "💡 To run with a specific job:"
echo "   act push -W .github/workflows/build-and-publish.yml -j test -s GITHUB_TOKEN=\"\$(gh auth token)\""