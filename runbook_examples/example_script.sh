#!/bin/bash

DATA=$(</dev/stdin)
echo "stdin: $DATA, args: $@"

echo "text from the script into a new file" > action.txt
