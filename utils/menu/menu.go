package menu

import (
	"fmt"
)

type menu struct {
	first *option
	last  *option
}

type option struct {
	tag      string
	callback func()
	next     *option
}

func Create() *menu {
	newMenu := new(menu)
	newMenu.first = new(option)
	newMenu.last = newMenu.first
	return newMenu
}

func (menu *menu) Add(tag string, callback func()) {
	menu.last.tag = tag
	menu.last.callback = callback
	menu.last.next = new(option)
	menu.last = menu.last.next
}

func (menu *menu) Activate() {
	for {
		current := menu.first
		count := 1
		fmt.Println("------------------------")
		for {
			if current.callback == nil {
				break
			}
			fmt.Printf("%d) %s\n", count, current.tag)
			count++
			current = current.next
		}
		fmt.Println("------------------------")
		fmt.Print("Select: ")
		var output string
		fmt.Scanln(&output)

		current = menu.first
		count = 1
		for {
			if output == fmt.Sprint(count) {
				fmt.Print("------------------------\n\n")
				current.callback()
				fmt.Print("\n")
				return
			}
			count++
			current = current.next
			if current == nil || current.callback == nil {
				fmt.Print("Invalid option.\n")
				break
			}
		}
	}
}

func SliceSelect[T any](slice []T) (index int) {
	if len(slice) == 0 {
		return -1
	}
	for {
		for i, elem := range slice {
			fmt.Printf("  %d - %+v\n", i, elem)
		}
		fmt.Print("\nSelect index: ")
		var output string
		fmt.Scanln(&output)
		for i := range slice {
			if output == fmt.Sprint(i) {
				return i
			}
		}
		fmt.Print("Invalid option.\n\n")
	}
}
