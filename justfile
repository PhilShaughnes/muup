set dotenv-load := true

default:
	@just --list

build:
	go build -o muup .

run:
	go run . --config config.toml --db muup.db --port 8080

test:
	go test -v ./...

build-linux:
	GOOS=linux GOARCH=amd64 go build -o muup-linux .

deploy: build-linux
	ssh wx.lan 'mkdir -p ~/.local/bin ~/.config/muup ~/.local/share/muup'
	rsync -avz muup-linux wx.lan:~/.local/bin/muup
	rsync -avz config.toml wx.lan:~/.config/muup/

deploy-service:
	rsync -avz muup.service wx.lan:~/.config/systemd/user/
	ssh wx.lan 'systemctl --user daemon-reload && systemctl --user enable muup'

restart:
	ssh wx.lan 'systemctl --user restart muup'

ship: deploy restart

logs:
	ssh wx.lan 'journalctl --user -u muup -f'

clean:
	rm -f muup muup-linux muup.db
