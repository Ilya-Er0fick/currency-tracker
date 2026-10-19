// Пакет auth отвечает за пароли и сессионные токены.
package auth

import "golang.org/x/crypto/bcrypt"

// Хеширование намеренно медленное: это и есть защита от перебора.
// Стоимость подобрана так, чтобы проверка занимала десятки миллисекунд —
// незаметно при входе и дорого при подборе.
const passwordCost = 12

// dummyHash используется, когда пользователь не найден: проверка всё равно
// выполняется, иначе по времени ответа можно было бы узнать, существует ли
// учётная запись с таким адресом.
var dummyHash = []byte("$2a$12$C6UzMDM.H6dfI/f/IKcEe.3O2m8oF4EjtZ2E0b3WzQ0RfEoKxcM3K")

func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), passwordCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// CheckPasswordDummy тратит столько же времени, сколько обычная проверка,
// но всегда заканчивается отказом.
func CheckPasswordDummy(plain string) {
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(plain))
}
