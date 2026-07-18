# bash completion for study — installed by `make install` to
# ~/.local/share/bash-completion/completions/study
_study() {
    local cur prev
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    case "$prev" in
        --order)
            COMPREPLY=( $(compgen -W "adaptive sequential flip-through weak-only" -- "$cur") )
            return ;;
        --answer-mode)
            COMPREPLY=( $(compgen -W "type choice" -- "$cur") )
            return ;;
        --time-limit|--wrong-pause)
            COMPREPLY=( $(compgen -W "none" -- "$cur") )
            return ;;
        --ahead|--new-per-session)
            COMPREPLY=( $(compgen -W "all" -- "$cur") )
            return ;;
        --font-size)
            COMPREPLY=( $(compgen -W "small medium large x-large" -- "$cur") )
            return ;;
        --audio-speed)
            return ;;
        --watch|--unwatch)
            COMPREPLY=( $(compgen -d -- "$cur") )
            return ;;
    esac

    if [[ "$cur" == -* ]]; then
        COMPREPLY=( $(compgen -W "--reverse --order --answer-mode --ahead --time-limit --wrong-pause --preview-new --new-per-session --font-size --audio-speed --stats --forget --watch --unwatch --library --help" -- "$cur") )
        return
    fi

    # Deck argument: .deck files, and directories (packs are *.deck dirs).
    local IFS=$'\n'
    COMPREPLY=( $(compgen -d -- "$cur") $(compgen -f -X '!*.deck' -- "$cur") )
}

complete -o filenames -F _study study
