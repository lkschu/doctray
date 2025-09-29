#!/bin/bash

tmux -u new-window -t "gogindoctray" -n 'GO' 'zsh'
tmux -u new-window -t "gogindoctray" -n 'VIM' 'vim main.go; zsh'
tmux -u new-window -t "gogindoctray" -n '?' 'zsh'

# tmux -u at -t "ziglings_org"
