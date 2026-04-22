# tmux-shunpo

Harpoon2-style tmux navigation. Instant marks + per-session tool slots.

> ⚠️ **WIP — Not working yet**
>
> This project is under active development. The `trunk` branch contains the
> v0.1.0 revamp in progress. Nothing is usable yet. Do not install or rely on
> this code until a release is tagged.

## Features (planned)

- **Marks (1-9)** — instant jump to bookmarked directories via sesh
- **Tools (@1-@9)** — per-session persistent window slots with bound commands
- **Search** — fuzzy session picker powered by sesh + fzf/sk
- **Gum editors** — interactive mark/tool editing in tmux popups

## Dependencies

- bash >= 4.0, tmux >= 3.2, [sesh](https://github.com/joshmedeski/sesh), [yq](https://github.com/mikefarah/yq) (Go), [gum](https://github.com/charmbracelet/gum), fzf or sk

## License

MIT