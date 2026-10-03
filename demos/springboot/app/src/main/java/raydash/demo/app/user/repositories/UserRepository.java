package raydash.demo.app.user.repositories;

import org.springframework.data.jpa.repository.JpaRepository;

import raydash.demo.app.user.models.UserEntity;

public interface UserRepository extends JpaRepository<UserEntity, Long> {}
