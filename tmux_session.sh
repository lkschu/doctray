#!/bin/bash

tmux -u new-window -t "gogindoctray" -n 'GO' 'bash -c "source ./DOC-TRAY_ENVIRONMENT.sh; go run main.go"; zsh; zsh'
tmux -u new-window -t "gogindoctray" -n 'VIM' 'vim main.go; zsh'
tmux -u new-window -t "gogindoctray" -n '?' 'zsh'

# tmux -u at -t "ziglings_org"
