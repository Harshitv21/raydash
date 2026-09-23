package raydash.demo.app.user.services.impl;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.cache.annotation.Cacheable;
import org.springframework.stereotype.Service;

import raydash.demo.app.user.dtos.response.UserDetailResponseDto;
import raydash.demo.app.user.mappers.UserMapper;
import raydash.demo.app.user.models.UserEntity;
import raydash.demo.app.user.repositories.UserRepository;
import raydash.demo.app.user.services.UserService;

@Service 
public class UserImpl implements UserService {
    private static final Logger log = LoggerFactory.getLogger(UserImpl.class);    

    private final UserRepository userRepositoryObject;

    public UserImpl(UserRepository userRepositoryObject) {
        this.userRepositoryObject = userRepositoryObject;
    }

    @Override
    @Cacheable(value = "users", key = "#id")
    public UserDetailResponseDto getUserDetails(Long id) {
        log.info("[TEST_APP] Cache miss! Key not found. Fetching from DB");

        UserEntity user = userRepositoryObject.findById(id).orElseThrow();
        return UserMapper.toUserDetailResponseDto(user);
    }
}
